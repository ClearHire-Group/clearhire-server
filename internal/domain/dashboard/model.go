package dashboard

// Metrics é o resumo numérico do topo do Dashboard (ver clearhire-app pages/dashboard) — sempre
// computado por query (mesma regra de migrations/0001_init.sql, seção "MÉTRICAS COMPUTADAS"),
// nunca persistido. Os dois *Trend são reconstruídos a partir de timestamps de ciclo de vida
// (opened_at/closed_at das campanhas, created_at/rejected_at dos candidatos) porque não existe
// tabela de snapshot histórico — são aproximação, não fato registrado. Ver repository.go pro
// porquê exato de cada aproximação.
type Metrics struct {
	ActiveCampaigns                 int
	ActiveCampaignsTrendLabel       string
	ActiveCampaignsTrend            []int
	CandidatesInProcess             int
	CandidatesInProcessContextLabel string
	HiresInPeriod                   int
	// HiresGoal não é computado — vem de companies.hiring_goal_per_period (migrations/0004).
	HiresGoal               int
	AvgFunnelDays           int
	AvgFunnelDaysTrendLabel string
	AvgFunnelDaysTrend      []int
}

// Suggestion é uma linha do card "Pendências de revisão" do Dashboard. Não é persistida nem gerada
// por LLM: sai de consulta + regra determinística sobre avaliações que a IA já gravou (service.go,
// GetSuggestions). O texto é montado aqui, no servidor, a partir de dado do banco — nome, número
// e fase nunca são inventados.
type Suggestion struct {
	ID                 string
	Message            string
	PrimaryActionLabel string
	PrimaryActionRoute []string
	Highlighted        bool
}

// ReadyCandidate é um candidato aguardando decisão do RH cuja última avaliação de IA, na fase em
// que ele está agora, passou do limiar de match (repository.go, ReadyToAdvance).
type ReadyCandidate struct {
	CandidateID   string
	CandidateName string
	CampaignID    string
	CampaignTitle string
	PhaseKey      string
	// NextPhaseKey vazio quando a fase atual é a última do funil daquela campanha.
	NextPhaseKey string
	MatchPct     int
}

// WaitingGroup é quantos candidatos de uma campanha aguardam decisão do RH numa mesma fase.
type WaitingGroup struct {
	CampaignID    string
	CampaignTitle string
	PhaseKey      string
	Count         int
}
