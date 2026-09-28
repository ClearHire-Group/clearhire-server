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

// SuggestionResponse é o formato exposto em GET /dashboard/ai-suggestions — mesmo shape de
// AiSuggestion no front (clearhire-app/src/app/core/models.ts). PrimaryActionRoute é a lista de
// segmentos que o routerLink do Angular espera (["/campanhas", id, "candidatos", id]), não uma URL.
type SuggestionResponse struct {
	ID                 string   `json:"id"`
	Message            string   `json:"message"`
	PrimaryActionLabel string   `json:"primaryActionLabel"`
	PrimaryActionRoute []string `json:"primaryActionRoute"`
	Highlighted        bool     `json:"highlighted"`
}

func toSuggestionResponses(items []Suggestion) []SuggestionResponse {
	out := make([]SuggestionResponse, 0, len(items))
	for _, s := range items {
		out = append(out, SuggestionResponse{
			ID:                 s.ID,
			Message:            s.Message,
			PrimaryActionLabel: s.PrimaryActionLabel,
			PrimaryActionRoute: s.PrimaryActionRoute,
			Highlighted:        s.Highlighted,
		})
	}
	return out
}
