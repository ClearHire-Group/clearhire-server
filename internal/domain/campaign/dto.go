package campaign

// CreateCampaignRequest é o payload de POST /campaigns — tela Nova Campanha.
type CreateCampaignRequest struct {
	Title        string   `json:"title" validate:"required"`
	City         string   `json:"city"`
	State        string   `json:"state"`
	Modality     string   `json:"modality" validate:"required"`
	ContractType string   `json:"contractType" validate:"required"`
	Seniority    string   `json:"seniority" validate:"required"`
	PhaseKeys    []string `json:"phaseKeys" validate:"required"` // módulos escolhidos, na ordem
}

// TogglePauseResponse é o payload de resposta de POST /campaigns/:id/toggle-pause.
type TogglePauseResponse struct {
	Status Status `json:"status"`
}
