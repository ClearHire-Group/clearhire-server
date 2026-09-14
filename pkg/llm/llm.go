// Package llm define o contrato de extração de currículo — trocável de provedor de propósito
// (usa Claude agora, mas o resto do sistema nunca importa o pacote do provedor diretamente, só esta
// interface). Ver pkg/llm/anthropicadapter pra implementação concreta.
package llm

import (
	"context"
	"errors"
)

// Input é mutuamente exclusivo: exatamente um dos dois campos vem preenchido. PDFBytes nunca é
// gravado em disco — o handler HTTP lê o upload pra memória, passa direto aqui, e descarta depois
// da chamada.
type Input struct {
	Text     string
	PDFBytes []byte
}

// ExperienceEntry, SkillMention e LanguageMention espelham as tabelas candidate_experience_entries/
// candidate_skills/talent_languages de propósito — a extração alimenta as duas escritas (candidate e
// talent) com o mesmo formato.
type ExperienceEntry struct {
	Role, Company, PeriodLabel, Description string
}

type SkillMention struct {
	Term            string
	Level           string
	YearsExperience *int
}

type LanguageMention struct {
	Name        string
	Proficiency string
}

// ExtractedProfile é o que qualquer provedor precisa devolver. RawText é o texto que a IA
// efetivamente leu (nunca os bytes do PDF em si) — vira o registro de auditoria em
// talent_source_documents, no lugar de guardar o arquivo original.
type ExtractedProfile struct {
	Name, Email, Phone, City, State, LinkedInURL           string
	YearsExperience                                        *int
	Modality, Seniority                                    string
	SalaryMin, SalaryMax                                   *float64
	AvailableFrom                                          *string
	AvailabilityNote, Summary                              string
	EducationDegree, EducationInstitution, EducationPeriod string
	Experience                                             []ExperienceEntry
	Skills                                                 []SkillMention
	Sectors                                                []string
	Languages                                              []LanguageMention
	RawText                                                string
}

// Sentinelas pra o service decidir a mensagem certa pro candidato (nunca o erro cru do provedor) —
// ver candidate.Service.RegisterPublicApplication.
var (
	ErrRefused             = errors.New("llm: extração recusada pelo modelo")
	ErrMalformedOutput     = errors.New("llm: modelo não devolveu saída estruturada válida")
	ErrRateLimited         = errors.New("llm: provedor com limite de taxa atingido")
	ErrProviderUnavailable = errors.New("llm: provedor indisponível")
)

// Extractor é o único ponto de contato do resto do sistema com IA de extração de currículo.
type Extractor interface {
	Extract(ctx context.Context, in Input) (*ExtractedProfile, error)
}
