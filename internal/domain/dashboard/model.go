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
