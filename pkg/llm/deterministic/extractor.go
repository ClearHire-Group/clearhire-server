// Package deterministic é uma implementação de llm.Extractor SEM chamada de IA nenhuma — extrai
// só o que dá pra reconhecer com certeza via padrão de texto (e-mail, telefone, link do LinkedIn) e
// devolve o texto bruto pro resto do pipeline casar skills contra a taxonomia já cadastrada (ver
// candidate.Repository.FindSkillMentionsInText — dicionário, não IA).
//
// Existe porque a extração via IA custa por chamada de API e a decisão de produto, pelo menos pro
// MVP, foi não depender desse custo: é o provedor PADRÃO (LLM_PROVIDER=deterministic). Trocar para
// pkg/llm/groqadapter ou pkg/llm/anthropicadapter é só configuração (LLM_PROVIDER + chave), porque
// todos implementam a mesma interface llm.Extractor.
//
// Limitação real, de propósito: nome da pessoa, experiência profissional estruturada e formação
// não são coisas que dá pra extrair de forma confiável só com regex — por isso o formulário de
// candidatura sempre pede nome e e-mail explícitos mesmo no modo currículo (nunca dependem desta
// extração), e o modo manual continua sendo o caminho mais completo.
package deterministic

import (
	"context"
	"regexp"
	"strings"

	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

var (
	emailPattern    = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	phonePattern    = regexp.MustCompile(`\(?\d{2}\)?[\s.\-]?9?\d{4}[\s.\-]?\d{4}`)
	linkedinPattern = regexp.MustCompile(`(?i)linkedin\.com/in/[a-zA-Z0-9\-_/]+`)
)

type extractor struct{}

// New devolve um llm.Extractor determinístico — zero custo, zero rede, zero dependência de
// configuração (diferente do adapter da Claude, nunca falha por falta de chave de API).
func New() llm.Extractor {
	return &extractor{}
}

// ProviderName identifica este caminho em llm_usage. Linhas com este provedor e zero tokens são
// gratuitas de verdade — não é contabilidade faltando.
const ProviderName = "deterministic"

func (e *extractor) Extract(_ context.Context, in llm.Input) (*llm.ExtractedProfile, llm.Usage, error) {
	usage := llm.Free(ProviderName)

	text := in.Text
	if len(in.PDFBytes) > 0 {
		extracted, err := llm.ExtractPDFText(in.PDFBytes)
		if err != nil {
			return nil, usage, llm.ErrMalformedOutput
		}
		text = extracted
	}
	if strings.TrimSpace(text) == "" {
		return nil, usage, llm.ErrMalformedOutput
	}

	profile := &llm.ExtractedProfile{RawText: text}
	if m := emailPattern.FindString(text); m != "" {
		profile.Email = m
	}
	if m := phonePattern.FindString(text); m != "" {
		profile.Phone = m
	}
	if m := linkedinPattern.FindString(text); m != "" {
		profile.LinkedInURL = m
	}
	// Skills ficam de fora aqui de propósito — sem acesso ao banco, este pacote não sabe qual é a
	// taxonomia cadastrada. candidate.Service resolve isso depois, direto do RawText (ver
	// FindSkillMentionsInText), não a partir de ExtractedProfile.Skills.
	return profile, usage, nil
}
