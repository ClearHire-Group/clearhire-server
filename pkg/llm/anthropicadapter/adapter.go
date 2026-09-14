// Package anthropicadapter é a única implementação concreta de llm.Extractor hoje — usa a API da
// Claude (Messages, tool use forçado) pra ler um currículo (texto colado ou PDF nativo) e devolver
// dado estruturado. Nenhum outro pacote deste servidor importa o SDK da Anthropic diretamente; todo
// mundo fala com llm.Extractor.
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
)

const toolName = "extract_candidate_profile"

const extractionTimeout = 25 * time.Second

const instructions = `Você recebeu um currículo (texto colado ou um PDF). Leia com atenção e chame a
ferramenta extract_candidate_profile com os dados estruturados que conseguir identificar.

Regras:
- Nunca invente informação que não está no currículo. Campo que não aparece: string vazia,
  lista vazia, ou 0 pra número — nunca um valor chutado.
- "skills" são competências técnicas específicas (linguagens, ferramentas, módulos, frameworks),
  não soft skills genéricas.
- "rawText" é o texto completo do currículo, exatamente como você o leu, sem resumir nem editar —
  isso vira o registro de auditoria do que foi processado.
- "summary" é um resumo curto (2-3 frases) do perfil profissional da pessoa, na sua própria síntese.`

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

func (a *adapter) Extract(ctx context.Context, in llm.Input) (*llm.ExtractedProfile, error) {
	if a.model == "" {
		return nil, llm.ErrProviderUnavailable
	}

	ctx, cancel := context.WithTimeout(ctx, extractionTimeout)
	defer cancel()

	var content []anthropic.ContentBlockParamUnion
	switch {
	case len(in.PDFBytes) > 0:
		content = append(content, anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{
			Data: base64.StdEncoding.EncodeToString(in.PDFBytes),
		}))
	case in.Text != "":
		content = append(content, anthropic.NewTextBlock(in.Text))
	default:
		return nil, llm.ErrMalformedOutput
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
					Properties: extractionSchemaProperties,
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
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			if apiErr.StatusCode == 429 {
				return nil, llm.ErrRateLimited
			}
		}
		return nil, llm.ErrProviderUnavailable
	}

	if resp.StopReason == anthropic.StopReasonRefusal {
		return nil, llm.ErrRefused
	}

	for _, block := range resp.Content {
		if block.Type != "tool_use" {
			continue
		}
		var out extractionOutput
		if err := json.Unmarshal(block.Input, &out); err != nil {
			return nil, llm.ErrMalformedOutput
		}
		return out.toProfile(), nil
	}
	return nil, llm.ErrMalformedOutput
}
