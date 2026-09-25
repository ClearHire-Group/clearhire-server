// Package llm define os contratos de IA do sistema — extração de currículo (Extractor) e avaliação
// de candidato (Assessor) — trocáveis de provedor de propósito: o resto do sistema nunca importa o
// pacote de um provedor diretamente, só estas interfaces. Implementações: pkg/llm/groqadapter
// (extração + avaliação), pkg/llm/anthropicadapter (extração) e pkg/llm/deterministic (sem IA, custo
// zero). Quem escolhe é LLM_PROVIDER (internal/factory/llm_factory.go); limites e re-tentativa
// seguros vivem em limits.go e a validação da saída dos modelos em validate.go.
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
	// ErrUnsupportedInput é o provedor dizendo (antes de qualquer chamada) que não sabe ler aquele
	// tipo de entrada — hoje, PDF escaneado num provedor sem visão/PDF nativo. Nada foi cobrado.
	ErrUnsupportedInput = errors.New("llm: provedor não suporta este tipo de entrada")
	// ErrBudgetExceeded é recusa NOSSA, antes de qualquer chamada — a empresa atingiu o teto de
	// gasto do mês. Para o candidato é indistinguível de provedor fora do ar (cai na mesma
	// sugestão de usar o formulário manual), e isso é proposital: o estado financeiro da empresa
	// contratante não é informação que deva vazar para quem está se candidatando.
	ErrBudgetExceeded = errors.New("llm: teto de gasto do mês atingido")
)

// Usage é a conta da chamada, e é obrigatória no retorno de todo Extractor — não é telemetria
// opcional. Sem isto não há como responder "quanto custou esta campanha" nem perceber um pico de
// abuso, e a única alternativa seria descobrir pela fatura no fim do mês.
//
// Um provedor que não sabe informar tokens devolve zero. Isso é ambíguo com "foi de graça", e é por
// isso que Provider existe: `deterministic` com zero tokens é gratuito de fato, enquanto um provedor
// pago com zero tokens é sinal de que a contabilidade quebrou.
type Usage struct {
	Provider string
	Model    string
	// CachedInputTokens é subconjunto de InputTokens que veio de cache do provedor (mais barato),
	// não um valor somado por fora.
	InputTokens       int
	OutputTokens      int
	CachedInputTokens int
	// PromptVersion muda sempre que o prompt ou o schema mudam. É o que permite distinguir dado
	// produzido por versões diferentes e reprocessar seletivamente, em vez de tudo ou nada.
	PromptVersion string
	DurationMs    int
}

// Free é a conta de um caminho que não chamou provedor pago nenhum.
func Free(provider string) Usage { return Usage{Provider: provider} }

// Extractor é o único ponto de contato do resto do sistema com IA de extração de currículo.
type Extractor interface {
	Extract(ctx context.Context, in Input) (*ExtractedProfile, Usage, error)
}

// JobContext é o que o avaliador precisa saber da vaga. Vem só de campos que a empresa já preenche
// na campanha — não existe critério estruturado ainda (campaign_match_criteria não tem escritor).
type JobContext struct {
	Title, Seniority, Modality                  string
	Description, Responsibilities, Requirements string
}

// CandidateContext é o perfil estruturado do candidato, SEM identificação. Nome, e-mail, telefone e
// LinkedIn ficam de fora de propósito e por três motivos: minimização de dados (o provedor não
// precisa saber quem é a pessoa para julgar aderência), menos tokens e menos viés (a nota não pode
// depender de quem a pessoa é, só do que ela fez).
type CandidateContext struct {
	YearsExperience *int
	Summary         string
	Education       string
	Experience      []ExperienceEntry
	Skills          []string
}

type AssessInput struct {
	Job       JobContext
	Candidate CandidateContext
}

// Assessment é a sugestão da IA para o recrutador. É sempre uma sugestão: nada no sistema decide
// sozinho a partir dela (Decide é ação manual do RH — invariante do produto e exigência de revisão
// humana de decisão automatizada da LGPD).
type Assessment struct {
	MatchPct      int
	MatchLabel    string
	MatchNote     string
	Justification string
	Strengths     []string
	Concerns      []string
}

// Assessor é o único ponto de contato do resto do sistema com a IA de avaliação de candidato.
// Mesmas regras do Extractor: Usage é obrigatório no retorno, inclusive em erro pós-resposta.
type Assessor interface {
	Assess(ctx context.Context, in AssessInput) (*Assessment, Usage, error)
	// PromptVersion identifica o prompt+schema em uso. É conhecido ANTES da chamada porque é o que
	// torna a avaliação idempotente: se já existe uma para (candidato, fase, esta versão), não se
	// paga de novo. Mudar o prompt muda a versão, e só isso autoriza reavaliar.
	PromptVersion() string
}
