package candidate

import "time"

type Status string

const (
	StatusAwaitingAI       Status = "aguardando_triagem_ia"
	StatusUnderReview      Status = "em_analise"
	StatusAwaitingDecision Status = "aguardando_decisao"
	StatusRejected         Status = "reprovado"
	StatusProposal         Status = "proposta"
	StatusHired            Status = "contratado"
)

// Chaves de fase do funil — espelham o enum phase_key do Postgres e as constantes de
// internal/domain/campaign/model.go. Duplicado de propósito (mesmo motivo de generateOpaqueToken
// em user/auth): candidate e campaign são domínios pares, nenhum importa o outro.
const (
	PhaseRecebidos    = "recebidos"
	PhaseFit          = "fit"
	PhaseTecnica      = "tecnica"
	PhaseEntrevista   = "entrevista"
	PhaseSelecionados = "selecionados"
)

// goesToBank espelha rejection_reasons.goes_to_bank (Tabela 1 do documento, mesmos dados da seed
// migration 0002 e de clearhire-app/src/app/core/models.ts REJECTION_REASONS) — mantido aqui
// também pra decidir o fluxo de reprovação sem um round-trip a mais no banco só pra ler um bool
// fixo que não muda em runtime.
var goesToBank = map[string]bool{
	"perdeu_outro_candidato":    true,
	"senioridade_acima":         true,
	"faltou_skill":              true,
	"pretensao_acima_budget":    true,
	"timing_indisponibilidade":  true,
	"reprovacao_tecnica":        false,
	"fit_cultural_incompativel": false,
}

// Candidate é a participação de uma pessoa numa campanha específica — nunca
// a pessoa em si (ver internal/domain/talent e db/schema.sql, seção 5).
type Candidate struct {
	ID                 string
	CompanyID          string
	CampaignID         string
	TalentID           *string
	Name               string
	Email              string
	City               string
	State              string
	YearsExperience    *int
	PhaseKey           string
	Status             Status
	RejectionReasonKey *string
	// MatchPct vem da candidate_ai_assessments mais recente — nil quando o candidato ainda não
	// teve nenhuma avaliação de IA (ex.: acabou de chegar em 'recebidos').
	MatchPct  *int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ExperienceEntry struct {
	Role        string
	Company     string
	PeriodLabel string
	Description string
	Position    int
}

// AIAssessment é a avaliação da IA pra fase ATUAL do candidato (candidate_ai_assessments é
// histórico — sempre a mais recente DA FASE ATUAL por created_at; ver AssessmentHistory pra
// fases anteriores). nil em CandidateDetail quando o candidato ainda não foi avaliado nesta fase.
type AIAssessment struct {
	MatchPct      int
	MatchLabel    string
	MatchNote     string
	Justification string
	Strengths     []string
	Concerns      []string
	// Confidence é o único discriminador de estado (ver migrations/0013): 'alta'|'media'|'baixa'
	// são graus de uma conclusão real; 'insuficiente' significa que a IA optou por não concluir
	// nesta fase — MatchPct/Strengths/Concerns continuam preenchidos (o schema estrito do
	// provedor exige), mas quem lê deve tratá-los como não confiáveis quando Confidence é
	// 'insuficiente', olhando StageInsight/MissingInformation em vez disso. Vazio em avaliações
	// gravadas antes da migration 0013 (prompt v1/v2, sem este campo) — tratado como 'alta'
	// implícito por quem lê, nunca como 'insuficiente' (não existe base pra inferir isso agora).
	Confidence string
	// StageInsight é o insight CURTO e específico do foco desta fase (ver assessment.go,
	// stageFocus) — distinto de Justification, que é a justificativa geral de sempre. Vazio nas
	// fases sem foco definido (recebidos, selecionados).
	StageInsight string
	// MissingInformation é o que faltou pra concluir com mais confiança nesta fase — preenchido
	// nos dois estados de Confidence, não só em 'insuficiente'.
	MissingInformation []string
	// ComparisonFlag compara com a fase anterior AVALIADA deste candidato (não com a campanha
	// inteira): 'reforca_anterior'|'diverge_anterior'|'novo', ou vazio quando não há fase
	// anterior avaliada pra comparar.
	ComparisonFlag string
}

// AIAssessmentHistoryEntry é uma avaliação passada do candidato, com a fase a que ela pertence —
// o que AssessmentHistory devolve pra reconstruir a trilha completa (ver comentário em
// migrations/0001_init.sql, candidate_ai_assessments: "é o que dá a trilha de 'o que a IA dizia
// quando aprovamos essa pessoa'", nunca lida de volta até esta migration).
type AIAssessmentHistoryEntry struct {
	PhaseKey   string
	Assessment AIAssessment
	CreatedAt  time.Time
}

// AssessmentOrigin é quem produziu uma avaliação: o que torna cada recomendação auditável depois
// (ver migrations/0010).
type AssessmentOrigin struct {
	Provider, Model, PromptVersion string
}

// CandidateDetail é tudo que a tela de perfil (GET /candidates/:id) precisa — Candidate mais
// contato, resumo, educação, experiência, skills e a avaliação de IA mais recente. ListByCampaign
// devolve só []Candidate (mais leve — é a listagem em coluna de fase, não a tela de perfil).
type CandidateDetail struct {
	Candidate
	Phone                string
	LinkedInURL          string
	Summary              string
	EducationDegree      string
	EducationInstitution string
	EducationPeriod      string
	Experience           []ExperienceEntry
	Skills               []string
	AI                   *AIAssessment
	// AIHistory é toda avaliação já gravada deste candidato, incluindo a da fase atual (dto.go
	// filtra a fase atual antes de expor — ver toCandidateProfileResponse), mais antiga primeiro.
	AIHistory []AIAssessmentHistoryEntry
}

// Decision é uma linha de candidate_decisions — o evento de domínio "RH decidiu": nunca só o
// estado final do candidato, sempre o registro de quem decidiu o quê, contra qual avaliação de IA
// (ver comentário da tabela na migration pro porquê disso importar pra métrica de confiança na IA).
type Decision struct {
	CompanyID          string
	CandidateID        string
	DecidedByUserID    string
	AssessmentID       *string
	Decision           string // "avancar" | "reprovar"
	FromPhase          string
	ToPhase            *string
	RejectionReasonKey *string
}

// TalentSeed é o que se grava em talents nos dois pontos de entrada que o domínio candidate
// controla — reprovação qualificada (campos mínimos: nome/e-mail/localização) e candidatura
// pública (perfil rico, extraído pela IA ou preenchido manualmente). Campos que uma reprovação
// nunca preenche (modalidade, senioridade, salário...) ficam zero-value ali, sem problema — a
// query de CreateTalentFromRejection só referencia as colunas que ela de fato usa.
// Nunca referencia a coluna embedding: ela nem existe no Postgres de dev local (pgvector não está
// instalado aqui, ver CLAUDE.md do servidor) e mesmo em produção é preenchida depois, por um job de
// ingestão de IA — não nesta escrita síncrona de request HTTP.
type TalentSeed struct {
	CompanyID                                              string
	Name                                                   string
	Email                                                  string
	Phone                                                  string
	LinkedInURL                                            string
	City                                                   string
	State                                                  string
	Modality                                               string
	Seniority                                              string
	YearsExperience                                        *int
	SalaryMin, SalaryMax                                   *float64
	AvailableFrom                                          *string
	AvailabilityNote                                       string
	Summary                                                string
	EducationDegree, EducationInstitution, EducationPeriod string
	Origin                                                 string
	LegalBasis                                             string
	ConsentState                                           string
	RecruiterNotes                                         string
}

// CandidateSeed é o que se grava em candidates numa candidatura pública — phase_key/status ficam
// nos defaults do banco ('recebidos'/'aguardando_triagem_ia'), igual qualquer candidato que acabou
// de chegar por qualquer outro caminho.
type CandidateSeed struct {
	CompanyID, CampaignID, TalentID                        string
	Name, Email, Phone, LinkedInURL, City, State           string
	YearsExperience                                        *int
	Summary                                                string
	EducationDegree, EducationInstitution, EducationPeriod string
}

// PublicCampaignRef é reconfirmado pelo domínio candidate no momento da escrita — nunca confia
// que quem chamou já validou a elegibilidade da campanha antes (mesma disciplina de nunca confiar
// em company_id vindo do cliente). Duplicado da checagem que campaign.Repository.FindPublicByID já
// faz, mesma convenção de phaseLabels/PhaseRecebidos já duplicados neste pacote.
type PublicCampaignRef struct {
	CompanyID string
}

// ManualApplicationFields é o modo "preencher manualmente" da candidatura pública — o mais
// confiável dos dois, porque não depende do extrator determinístico ter reconhecido tudo certo.
// Name não mora aqui — é campo de PublicApplicationInput, comum aos dois modos (ver comentário lá).
type ManualApplicationFields struct {
	Phone, LinkedInURL, City, State                        string
	YearsExperience                                        *int
	Summary                                                string
	EducationDegree, EducationInstitution, EducationPeriod string
	Experience                                             []ExperienceEntry
	Skills                                                 []string
}

// PublicApplicationInput é o que o handler monta (do JSON ou do multipart) antes de chamar
// Service.RegisterPublicApplication — exatamente um de Manual/ResumeText/PDFBytes vem preenchido.
type PublicApplicationInput struct {
	Consent  bool
	Honeypot string
	// Name e Email são sempre exigidos, nos dois modos — nome não é algo que dá pra extrair de
	// forma confiável só com reconhecimento de padrão (ver pkg/llm/deterministic), e
	// consentimento/contato não pode depender só de o texto ter sido lido certo.
	Name       string
	Email      string
	Manual     *ManualApplicationFields
	ResumeText string
	PDFBytes   []byte
}

// ResolvedSkill é um termo de skill já casado contra a taxonomia (skills/skill_synonyms) — termos
// que não batem com nada não viram ResolvedSkill, vão pra fila de revisão em vez disso (ver
// Repository.ResolveSkills/QueueSkillReview).
type ResolvedSkill struct {
	SkillID         string
	Level           string
	YearsExperience *int
}
