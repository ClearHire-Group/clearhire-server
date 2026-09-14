package dashboard

// Response é o formato exposto em GET /dashboard/metrics — mesmo shape de DashboardMetrics no
// front (clearhire-app/src/app/core/models.ts), sempre por DTO, nunca o model do domínio direto
// (ver clearhire-server/CLAUDE.md, "handler devolvendo model direto no JSON").
type Response struct {
	ActiveCampaigns                 int    `json:"activeCampaigns"`
	ActiveCampaignsTrendLabel       string `json:"activeCampaignsTrendLabel"`
	ActiveCampaignsTrend            []int  `json:"activeCampaignsTrend"`
	CandidatesInProcess             int    `json:"candidatesInProcess"`
	CandidatesInProcessContextLabel string `json:"candidatesInProcessContextLabel"`
	HiresInPeriod                   int    `json:"hiresInPeriod"`
	HiresGoal                       int    `json:"hiresGoal"`
	AvgFunnelDays                   int    `json:"avgFunnelDays"`
	AvgFunnelDaysTrendLabel         string `json:"avgFunnelDaysTrendLabel"`
	AvgFunnelDaysTrend              []int  `json:"avgFunnelDaysTrend"`
}

func toResponse(m *Metrics) *Response {
	return &Response{
		ActiveCampaigns:                 m.ActiveCampaigns,
		ActiveCampaignsTrendLabel:       m.ActiveCampaignsTrendLabel,
		ActiveCampaignsTrend:            m.ActiveCampaignsTrend,
		CandidatesInProcess:             m.CandidatesInProcess,
		CandidatesInProcessContextLabel: m.CandidatesInProcessContextLabel,
		HiresInPeriod:                   m.HiresInPeriod,
		HiresGoal:                       m.HiresGoal,
		AvgFunnelDays:                   m.AvgFunnelDays,
		AvgFunnelDaysTrendLabel:         m.AvgFunnelDaysTrendLabel,
		AvgFunnelDaysTrend:              m.AvgFunnelDaysTrend,
	}
}
