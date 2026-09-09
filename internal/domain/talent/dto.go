package talent

// RegisterManualRequest é o payload de POST /talents — cadastro manual, fora
// de campanha (LinkedIn, evento, indicação). Entra com legal_basis =
// legitimo_interesse e consent_state = nao_notificado.
type RegisterManualRequest struct {
	Name           string `json:"name" validate:"required"`
	RawProfileText string `json:"rawProfileText" validate:"required"`
	ContextNote    string `json:"contextNote"`
}

// SearchRequest é o payload de GET /talents/search?q=... — busca em
// linguagem natural (seção 8.1 do documento de especificação).
type SearchRequest struct {
	Query string `query:"q" validate:"required"`
}
