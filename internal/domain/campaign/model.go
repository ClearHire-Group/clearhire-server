package campaign

import "time"

type Status string

const (
	StatusActive  Status = "ativa"
	StatusPaused  Status = "pausada"
	StatusClosed  Status = "encerrada"
)

// Campaign é a vaga (ver db/schema.sql, tabela campaigns).
type Campaign struct {
	ID              string
	CompanyID       string
	CreatedByUserID string
	Title           string
	City            string
	State           string
	Modality        string
	ContractType    string
	Seniority       string
	Status          Status
	OpenedAt        time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Phase é um módulo do funil configurado pra essa campanha (campaign_phases).
type Phase struct {
	Key      string
	Position int
}
