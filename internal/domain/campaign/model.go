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
	Description     string
	// Seções estruturadas da vaga (migration 0006) — exibidas ao candidato no link público.
	// Opcionais; só Description trava a geração do link.
	Responsibilities string
	Requirements     string
	Benefits         string
	City             string
	State            string
	Modality         string
	ContractType     string
	Seniority        string
	Status           Status
	// AcceptsPublicApplications é independente de Status — pausar/encerrar a campanha já derruba
	// o link (a query pública sempre checa as duas colunas juntas), mas o recrutador também pode
	// ligar/desligar só o link de candidatura sem mexer no status da campanha em si.
	AcceptsPublicApplications bool
	OpenedAt                  time.Time
	PausedAt                  *time.Time
	ClosedAt                  *time.Time
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
}

// PublicInfo é o subconjunto seguro de Campaign pra exibir num link público — nunca
// CreatedByUserID, contagem de candidatos ou qualquer coisa interna. Ver campaign.Repository.FindPublicByID.
type PublicInfo struct {
	ID               string
	Title            string
	CompanyName      string
	Description      string
	Responsibilities string
	Requirements     string
	Benefits         string
	City             string
	State            string
	Modality         string
	ContractType     string
	Seniority        string
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

// ReportPeriod recorta a tela Relatórios por DATA DA CANDIDATURA (candidates.created_at), nunca
// por "como o funil estava naquele dia": o sistema não guarda histórico de fase, então o relatório
// de um mês passado responde "dos que se candidataram naquele mês, onde eles estão HOJE" — a
// pergunta que os dados existentes conseguem responder de verdade. Reconstruir o funil retroativo
// exigiria snapshot histórico, que não existe (mesma limitação já documentada no dashboard).
//
// Instantes, não datas: quem escolhe as bordas é o front, que conhece o fuso do usuário. Assim
// nenhuma query aqui precisa assumir um timezone. From é inclusivo, To é EXCLUSIVO (um mês vai do
// dia 1º 00:00 ao dia 1º do mês seguinte 00:00). Nil em qualquer um dos dois = sem aquele limite;
// o zero value, portanto, é "todo o período".
type ReportPeriod struct {
	From *time.Time
	To   *time.Time
}
