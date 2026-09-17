package activity

import "time"

// Response é o formato exposto em GET /dashboard/activity — mesmo shape de ActivityItem no front
// (clearhire-app/src/app/core/models.ts).
//
// CreatedAt sai como timestamp ISO, não como rótulo pronto ("há 12 minutos"): formatar tempo
// relativo aqui congelaria o texto no instante da resposta — uma aba aberta por uma hora
// continuaria dizendo "há 12 minutos". Quem deriva o rótulo é o front, a cada render
// (core/relative-time.ts).
type Response struct {
	ID            string    `json:"id"`
	Actor         string    `json:"actor"`
	Message       string    `json:"message"`
	CampaignID    *string   `json:"campaignId,omitempty"`
	CampaignTitle *string   `json:"campaignTitle,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

func toResponse(items []Item) []Response {
	out := make([]Response, 0, len(items))
	for _, it := range items {
		out = append(out, Response{
			ID:            it.ID,
			Actor:         it.Actor,
			Message:       it.Message,
			CampaignID:    it.CampaignID,
			CampaignTitle: it.CampaignTitle,
			CreatedAt:     it.CreatedAt,
		})
	}
	return out
}
