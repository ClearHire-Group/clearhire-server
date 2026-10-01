// Package groqadapter implementa llm.Extractor e llm.Assessor sobre a API da Groq (formato
// compatível com o da OpenAI, via HTTP puro — sem SDK). Nenhum outro pacote deste servidor conhece
// a Groq; todo mundo fala com as interfaces de pkg/llm.
//
// Diferenças que importam frente ao adapter da Claude:
//   - A Groq NÃO combina structured outputs com tool use, então o contrato de saída é imposto por
//     response_format json_schema estrito (só existe em alguns modelos — ver SupportedModel), não
//     por tool forçada.
//   - Não lê PDF. PDF com camada de texto já chega aqui como texto (llm.PreprocessInput); PDF
//     escaneado devolve llm.ErrUnsupportedInput.
//   - Tem limite de tokens POR MINUTO por chave: uma requisição grande demais nunca passa, então há
//     um teto local de entrada em vez de deixar a chamada falhar depois de gastar a cota.
package groqadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// ProviderName identifica este caminho em llm_usage.
const ProviderName = "groq"

const defaultEndpoint = "https://api.groq.com/openai/v1/chat/completions"

// attemptTimeout é o teto de UMA chamada. A re-tentativa e o teto total ficam em llm.Limits.
const attemptTimeout = 15 * time.Second

// maxInputChars é o teto local do texto enviado — o mesmo do resto do pipeline (o formulário e
// llm.PreprocessInput já garantem isto; aqui é a última barreira). ~14.000 caracteres de português
// são ~4K tokens; com as instruções e a saída, uma chamada fica abaixo do limite de 8K tokens/minuto
// do free tier.
const maxInputChars = llm.MaxResumeTextChars

// supportedModels são os que aceitam json_schema estrito (docs da Groq, conferidas em 2026-09-25).
// Lista fechada de propósito: um modelo fora dela cairia no modo JSON sem validação de schema, que
// pode devolver JSON válido no formato errado.
var supportedModels = map[string]bool{
	"openai/gpt-oss-20b":  true,
	"openai/gpt-oss-120b": true,
	"qwen/qwen3.8-27b":    true,
}

// SupportedModel diz se o modelo pode ser usado por este adapter. O boot usa isto para recusar uma
// configuração que só falharia em produção, na primeira candidatura.
func SupportedModel(model string) bool { return supportedModels[model] }

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type jsonSchemaFormat struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type responseFormat struct {
	Type       string           `json:"type"`
	JSONSchema jsonSchemaFormat `json:"json_schema"`
}

type chatRequest struct {
	Model               string         `json:"model"`
	Messages            []message      `json:"messages"`
	ResponseFormat      responseFormat `json:"response_format"`
	MaxCompletionTokens int            `json:"max_completion_tokens"`
	Temperature         float64        `json:"temperature"`
	ReasoningEffort     string         `json:"reasoning_effort,omitempty"`
	IncludeReasoning    *bool          `json:"include_reasoning,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// Adapter implementa llm.Extractor e llm.Assessor.
type Adapter struct {
	apiKey   string
	model    string
	endpoint string
	http     *http.Client
	timeout  time.Duration
}

// NewWithEndpoint é New com outro endpoint compatível com a API da Groq — para apontar um proxy
// interno ou um servidor falso em testes de ponta a ponta. A chave vai para lá, então só o operador
// (variável de ambiente) escolhe isto; nunca dado vindo de requisição.
func NewWithEndpoint(apiKey, model, endpoint string) *Adapter {
	a := New(apiKey, model)
	if endpoint != "" {
		a.endpoint = endpoint
	}
	return a
}

// New cria o adapter. Chave e modelo vazios são recusados no boot (internal/config), não aqui —
// mas Extract/Assess ainda respondem ErrProviderUnavailable se chegarem a ser chamados assim.
func New(apiKey, model string) *Adapter {
	return &Adapter{apiKey: apiKey, model: model, endpoint: defaultEndpoint, http: &http.Client{}, timeout: attemptTimeout}
}

func (a *Adapter) usage(promptVersion string) llm.Usage {
	return llm.Usage{Provider: ProviderName, Model: a.model, PromptVersion: promptVersion}
}

func (a *Adapter) ready() bool { return a.apiKey != "" && a.model != "" }

// reasoning ajusta o esforço de raciocínio por família de modelo. Os modelos gpt-oss raciocinam por
// padrão e cobram esses tokens como saída; para extração/triagem o esforço mínimo basta e é a
// diferença entre uma chamada de centavos e uma que estoura o teto de tokens por minuto.
func (a *Adapter) reasoning(req *chatRequest) {
	switch {
	case strings.HasPrefix(a.model, "openai/gpt-oss"):
		hide := false
		req.ReasoningEffort, req.IncludeReasoning = "low", &hide
	case strings.HasPrefix(a.model, "qwen/"):
		req.ReasoningEffort = "none"
	}
}

// complete faz UMA chamada HTTP e a classifica. Tudo que sai daqui como erro é um sentinela de
// pkg/llm — nunca o corpo do erro do provedor, que pode ecoar trechos do currículo.
//
// Erro de transporte/status: não houve resposta útil, então não há tokens a registrar; o Usage volta
// zerado, distinguível de uma chamada cobrada.
func (a *Adapter) complete(ctx context.Context, req chatRequest) (*chatResponse, error) {
	if !a.ready() {
		return nil, llm.ErrProviderUnavailable
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, llm.ErrProviderUnavailable
	}

	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, llm.ErrProviderUnavailable
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, llm.ErrProviderUnavailable
	}
	defer func() { _ = resp.Body.Close() }() // só leitura: erro no Close não muda nada
	// Limite defensivo: uma resposta legítima tem poucos KB.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, llm.ErrProviderUnavailable
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		var out chatResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			// Chegou um 200 ilegível: não dá nem para ler o consumo. Tratado como indisponível.
			return nil, llm.ErrProviderUnavailable
		}
		return &out, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &llm.RateLimitError{RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		// Requisição maior que o limite do modelo/plano: re-tentar não adianta.
		return nil, llm.ErrUnsupportedInput
	case resp.StatusCode >= 500:
		return nil, llm.ErrProviderUnavailable
	default:
		// 400/401/403/404: erro nosso (chave, modelo, formato). Vira "indisponível" para o
		// candidato, mas precisa aparecer para quem opera — sem o corpo, que pode ter dado pessoal.
		log.Printf("groq: resposta %d — verifique GROQ_API_KEY/GROQ_MODEL e o formato do pedido", resp.StatusCode)
		return nil, llm.ErrProviderUnavailable
	}
}

func retryAfter(header string) time.Duration {
	secs, err := strconv.ParseFloat(strings.TrimSpace(header), 64)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

// account preenche o Usage a partir da resposta. Chamar SEMPRE antes de qualquer return de erro
// pós-resposta: resposta que chegou já foi cobrada, mesmo que inútil.
func account(u *llm.Usage, resp *chatResponse) {
	u.InputTokens = resp.Usage.PromptTokens
	u.OutputTokens = resp.Usage.CompletionTokens
	u.CachedInputTokens = resp.Usage.PromptTokensDetails.CachedTokens
}

// content extrai o JSON da resposta ou classifica por que não dá. Truncamento (finish_reason=length)
// é o caso clássico de "cobrado e inútil": o JSON vem cortado no meio.
func content(resp *chatResponse) (string, error) {
	if len(resp.Choices) == 0 {
		return "", llm.ErrMalformedOutput
	}
	choice := resp.Choices[0]
	if choice.Message.Refusal != "" {
		return "", llm.ErrRefused
	}
	if choice.FinishReason == "length" || strings.TrimSpace(choice.Message.Content) == "" {
		return "", llm.ErrMalformedOutput
	}
	return choice.Message.Content, nil
}

func decode(raw string, into any) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(into); err != nil {
		return errors.Join(llm.ErrMalformedOutput, err)
	}
	return nil
}

var tagPatterns = map[string]*regexp.Regexp{}

func init() {
	for _, tag := range []string{"curriculo", "vaga", "candidato"} {
		tagPatterns[tag] = regexp.MustCompile(`(?i)<\s*(/?)\s*` + tag + `\s*>`)
	}
}

// escapeTag impede que o conteúdo do candidato/vaga "feche" o delimitador e passe a parecer parte
// das instruções. Não é uma barreira completa (nenhuma é) — é o que torna o delimitador honesto.
// Regex e não busca por índice: caixa e espaços variam, e ToLower pode mudar o tamanho em bytes.
func escapeTag(s, tag string) string {
	return tagPatterns[tag].ReplaceAllString(s, "[${1}"+tag+"]")
}
