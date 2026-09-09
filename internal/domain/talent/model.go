package talent

import "time"

type ConsentState string

const (
	ConsentGiven       ConsentState = "consentido"
	ConsentNotNotified ConsentState = "nao_notificado"
	ConsentNotified    ConsentState = "notificado"
	ConsentOptOut      ConsentState = "oposicao_exclusao"
)

// Talent é a pessoa — existe independente de vaga e atravessa campanhas
// (ver db/schema.sql, tabela talents). NUNCA guarda score de match: isso é
// sempre recalculado contra os critérios de uma campanha específica.
type Talent struct {
	ID                string
	CompanyID         string
	Name              string
	Origin            string
	ConsentState      ConsentState
	ProfileReviewedAt time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
