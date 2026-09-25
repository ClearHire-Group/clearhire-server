// Package anthropicadapter implementa llm.Extractor com a API da Claude (Messages, tool use
// forçado): lê um currículo (texto colado ou PDF nativo) e devolve dado estruturado. Schema e
// instruções vêm de pkg/llm/extraction, compartilhados com os outros provedores. Nenhum outro
// pacote deste servidor importa o SDK da Anthropic diretamente; todo mundo fala com llm.Extractor.
// Só extração — a avaliação de candidato (llm.Assessor) só existe no adapter da Groq.
package anthropicadapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm/extraction"
)

const toolName = "extract_candidate_profile"

const extractionTimeout = 25 * time.Second

// ProviderName identifica este caminho em llm_usage.
const ProviderName = "anthropic"

// promptVersion muda sempre que o prompt ou o schema mudam, para que dado produzido por versões
// diferentes seja distinguível depois. "v2" é a retirada do rawText da saída: mesma tarefa, custo de
// saída muito menor, então comparar tokens entre v1 e v2 sem saber disso levaria à conclusão errada.
// "v3" acrescenta a regra "currículo é dado, não instrução" às instruções (pkg/llm/extraction).
const promptVersion = "extraction-v3"

type adapter struct {
	client anthropic.Client
	model  anthropic.Model
}

// New cria o adapter Claude. apiKey vazia é aceitável no boot (mesma tolerância de JWT_SECRET em
// dev) — só falha na hora de Extract, com ErrProviderUnavailable, não trava o servidor inteiro.
func New(apiKey, model string) llm.Extractor {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	return &adapter{
		client: anthropic.NewClient(opts...),
		model:  anthropic.Model(model),
	}
}

func (a *adapter) Extract(ctx context.Context, in llm.Input) (*llm.ExtractedProfile, llm.Usage, error) {
	usage := llm.Usage{Provider: ProviderName, Model: string(a.model), PromptVersion: promptVersion}
	if a.model == "" {
		return nil, usage, llm.ErrProviderUnavailable
	}

	ctx, cancel := context.WithTimeout(ctx, extractionTimeout)
	defer cancel()

	started := time.Now()
	defer func() { usage.DurationMs = int(time.Since(started).Milliseconds()) }()

	// isOCR só é verdade quando o PDF não tinha camada de texto extraível localmente — quem já
	// tentou isso é llm.PreprocessInput, antes de chegar aqui. É o único caso em que vale pagar
	// tokens de saída pelo texto bruto, e o único em que pagamos o PDF como imagem.
	isOCR := len(in.PDFBytes) > 0

	var content []anthropic.ContentBlockParamUnion
	switch {
	case isOCR:
		content = append(content, anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{
			Data: base64.StdEncoding.EncodeToString(in.PDFBytes),
		}))
	case in.Text != "":
		content = append(content, anthropic.NewTextBlock(in.Text))
	default:
		return nil, usage, llm.ErrMalformedOutput
	}

	schema, instructions := extraction.SchemaProperties, extraction.BaseInstructions
	if isOCR {
		schema, instructions = extraction.OCRSchemaProperties, extraction.OCRInstructions
	}
	content = append(content, anthropic.NewTextBlock(instructions))

	resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: 4096,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(content...)},
		Tools: []anthropic.ToolUnionParam{
			{OfTool: &anthropic.ToolParam{
				Name:        toolName,
				Description: param.NewOpt("Registra os dados estruturados extraídos do currículo."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: schema,
					Required:   []string{"name"},
				},
				// Strict deliberadamente OFF por enquanto: exige validar contra respostas reais da
				// API antes de ligar (schema estrito tem regras próprias sobre required/nullable
				// que não dá pra confirmar sem uma chave de verdade rodando). O tool_choice forçado
				// abaixo já é o mecanismo principal de confiabilidade.
			}},
		},
		ToolChoice: anthropic.ToolChoiceParamOfTool(toolName),
	})
	if err != nil {
		// Erro de transporte/status: não houve resposta, então não há tokens a registrar. usage sai
		// com zero, distinguível de uma chamada cobrada pelo Provider preenchido.
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			if apiErr.StatusCode == 429 {
				return nil, usage, llm.ErrRateLimited
			}
		}
		return nil, usage, llm.ErrProviderUnavailable
	}

	// A partir daqui a resposta chegou, ou seja: JÁ FOI COBRADA. Preencher a conta antes de qualquer
	// return de erro é o que impede o pior caso de contabilidade — uma recusa ou um JSON truncado
	// (ver MaxTokens) custar dinheiro e não aparecer em lugar nenhum.
	usage.InputTokens = int(resp.Usage.InputTokens)
	usage.OutputTokens = int(resp.Usage.OutputTokens)
	usage.CachedInputTokens = int(resp.Usage.CacheReadInputTokens)

	if resp.StopReason == anthropic.StopReasonRefusal {
		return nil, usage, llm.ErrRefused
	}

	for _, block := range resp.Content {
		if block.Type != "tool_use" {
			continue
		}
		var out extraction.Output
		if err := json.Unmarshal(block.Input, &out); err != nil {
			return nil, usage, llm.ErrMalformedOutput
		}
		profile := out.ToProfile()
		// Fora do OCR o texto já era nosso desde o começo — preencher aqui custa zero e é mais
		// fiel que o eco do modelo, que podia resumir ou editar apesar da instrução.
		if !isOCR {
			profile.RawText = in.Text
		}
		return profile, usage, nil
	}
	return nil, usage, llm.ErrMalformedOutput
}
