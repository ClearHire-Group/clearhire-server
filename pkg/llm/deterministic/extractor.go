// Package deterministic é uma implementação de llm.Extractor SEM chamada de IA nenhuma — extrai
// só o que dá pra reconhecer com certeza via padrão de texto (e-mail, telefone, link do LinkedIn) e
// devolve o texto bruto pro resto do pipeline casar skills contra a taxonomia já cadastrada (ver
// candidate.Repository.FindSkillMentionsInText — dicionário, não IA).
//
// Existe porque a extração via IA custa por chamada de API (cobrança à parte de qualquer
// assinatura de uso do Claude Code) e a decisão de produto, pelo menos pro MVP, foi não depender
// desse custo. pkg/llm/anthropicadapter continua existindo, intocado, pronto pra ligar de volta —
// trocar essa implementação pela outra é uma linha só em internal/factory/factory.go, porque as
// duas implementam a mesma interface llm.Extractor.
//
// Limitação real, de propósito: nome da pessoa, experiência profissional estruturada e formação
// não são coisas que dá pra extrair de forma confiável só com regex — por isso o formulário de
// candidatura sempre pede nome e e-mail explícitos mesmo no modo currículo (nunca dependem desta
// extração), e o modo manual continua sendo o caminho mais completo.
package deterministic

import (
	"bytes"
	"context"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"

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

func (e *extractor) Extract(_ context.Context, in llm.Input) (*llm.ExtractedProfile, error) {
	text := in.Text
	if len(in.PDFBytes) > 0 {
		extracted, err := extractPDFText(in.PDFBytes)
		if err != nil {
			return nil, llm.ErrMalformedOutput
		}
		text = extracted
	}
	if strings.TrimSpace(text) == "" {
		return nil, llm.ErrMalformedOutput
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
	return profile, nil
}

// extractPDFText lê os bytes em memória (nunca grava em disco — o handler HTTP também não grava,
// ver candidate/handler.go) e devolve o texto puro da camada de texto do PDF. Não é OCR: um PDF
// escaneado como imagem, sem texto selecionável, devolve string vazia — mesma limitação de
// qualquer extrator de texto de PDF sem visão computacional.
func extractPDFText(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	reader, err := r.GetPlainText()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		return "", err
	}
	return buf.String(), nil
}
