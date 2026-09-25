package groqadapter

import (
	"context"
	"unicode/utf8"

	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm/extraction"
)

// extractionPromptVersion muda sempre que o prompt ou o schema mudam (ver llm.Usage.PromptVersion).
const extractionPromptVersion = "extraction-groq-v1"

// extractionMaxOutputTokens deixa folga para o JSON de um currículo longo. Ficar curto demais é o
// pior erro: a saída trunca, a chamada é cobrada e não serve para nada.
const extractionMaxOutputTokens = 3000

// Extract lê um currículo em texto e devolve o perfil estruturado.
func (a *Adapter) Extract(ctx context.Context, in llm.Input) (*llm.ExtractedProfile, llm.Usage, error) {
	usage := a.usage(extractionPromptVersion)

	// PDF escaneado: sem camada de texto e a Groq não tem PDF nativo/visão. Recusado antes de gastar.
	if len(in.PDFBytes) > 0 {
		return nil, usage, llm.ErrUnsupportedInput
	}
	if in.Text == "" {
		return nil, usage, llm.ErrMalformedOutput
	}
	if utf8.RuneCountInString(in.Text) > maxInputChars {
		return nil, usage, llm.ErrUnsupportedInput
	}

	req := chatRequest{
		Model: a.model,
		Messages: []message{
			{Role: "system", Content: extraction.BaseInstructions},
			{Role: "user", Content: "<curriculo>\n" + escapeTag(in.Text, "curriculo") + "\n</curriculo>"},
		},
		ResponseFormat: responseFormat{Type: "json_schema", JSONSchema: jsonSchemaFormat{
			Name: "resume_extraction", Strict: true, Schema: extraction.StrictObjectSchema(extraction.SchemaProperties),
		}},
		MaxCompletionTokens: extractionMaxOutputTokens,
		Temperature:         0,
	}
	a.reasoning(&req)

	started := timeNow()
	resp, err := a.complete(ctx, req)
	usage.DurationMs = int(timeSince(started).Milliseconds())
	if err != nil {
		return nil, usage, err
	}

	// A partir daqui a resposta chegou, ou seja: JÁ FOI COBRADA.
	account(&usage, resp)
	raw, err := content(resp)
	if err != nil {
		return nil, usage, err
	}
	var out extraction.Output
	if err := decode(raw, &out); err != nil {
		return nil, usage, err
	}
	profile := out.ToProfile()
	// O texto já era nosso desde o começo — preencher aqui custa zero e é mais fiel que qualquer eco.
	profile.RawText = in.Text
	return profile, usage, nil
}
