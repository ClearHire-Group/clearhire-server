package candidate

// DecideRequest é o payload de POST /candidates/:id/decisions — o clique
// manual de aprovar/reprovar (ver db/schema.sql, tabela candidate_decisions).
// Nunca automático: a IA sugere, o RH decide (invariante do produto).
type DecideRequest struct {
	Decision           string  `json:"decision" validate:"required,oneof=avancar reprovar"`
	RejectionReasonKey *string `json:"rejectionReasonKey"`
	SendBankInvite     bool    `json:"sendBankInvite"`
}
