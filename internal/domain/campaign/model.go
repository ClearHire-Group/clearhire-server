package campaign

import "time"

type Status string

const (
	StatusActive Status = "ativa"
	StatusPaused Status = "pausada"
	StatusClosed Status = "encerrada"
)

// Chaves de fase do funil — espelham o enum phase_key do Postgres.
// Recebidos e Selecionados são fixos em toda campanha; os três do meio são
// módulos opcionais escolhidos (e ordenáveis) na criação (tela Nova
// Campanha, step 3).
const (
	PhaseRecebidos    = "recebidos"
	PhaseFit          = "fit"
	PhaseTecnica      = "tecnica"
	PhaseEntrevista   = "entrevista"
	PhaseSelecionados = "selecionados"
)

// Campaign é a vaga (ver migrations/0001_init.sql, tabela campaigns).
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
	PausedAt        *time.Time
	ClosedAt        *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Phase é um módulo do funil configurado pra essa campanha (campaign_phases).
type Phase struct {
	Key      string
	Position int
}

// PhaseCount é uma fase + sua contagem cumulativa de candidatos — sempre
// computada por query (ver migrations/0001_init.sql, seção "MÉTRICAS
// COMPUTADAS": uma coluna de cache foi tentada e removida de propósito),
// nunca uma coluna persistida. Cumulativa significa "candidatos cuja fase
// atual está nesta posição do funil OU adiante" — não "quem está exatamente
// aqui agora". Ver campaign/repository.go para o porquê disso ser necessário
// (a ordem das fases é por posição configurada, não pela ordem do enum).
type PhaseCount struct {
	CampaignID string
	Key        string
	Position   int
	Count      int
}
