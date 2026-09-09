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

// Candidate é a participação de uma pessoa numa campanha específica — nunca
// a pessoa em si (ver internal/domain/talent e db/schema.sql, seção 5).
type Candidate struct {
	ID       string
	CompanyID string
	CampaignID string
	TalentID *string
	Name     string
	Email    string
	PhaseKey string
	Status   Status
	CreatedAt time.Time
	UpdatedAt time.Time
}
