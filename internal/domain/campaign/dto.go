package campaign

import (
	"fmt"
	"math"
	"time"
)

// CreateCampaignRequest é o payload de POST /campaigns — tela Nova Campanha.
// PhaseKeys representa só os módulos OPCIONAIS escolhidos (fit/tecnica/entrevista), na ordem que
// o usuário organizou no step 3 — Recebidos e Selecionados são sempre implícitos, o service quem
// monta a lista completa. Lista vazia é um funil válido (mínimo: só Recebidos + Selecionados).
type CreateCampaignRequest struct {
	Title        string   `json:"title" validate:"required,max=200"`
	City         string   `json:"city" validate:"omitempty,max=100"`
	State        string   `json:"state" validate:"omitempty,max=100"`
	Modality     string   `json:"modality" validate:"required,oneof=remoto hibrido presencial"`
	ContractType string   `json:"contractType" validate:"required,oneof=clt pj estagio"`
	Seniority    string   `json:"seniority" validate:"required,oneof=junior pleno senior"`
	PhaseKeys    []string `json:"phaseKeys" validate:"omitempty,max=3,unique,dive,oneof=fit tecnica entrevista"`
}

// PhaseResponse espelha Phase do frontend (clearhire-app/src/app/core/models.ts) — key/num/label/count.
type PhaseResponse struct {
	Key   string `json:"key"`
	Num   int    `json:"num"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// Response espelha Campaign do frontend byte a byte — nunca devolver o model cru (sem json
// tags, vazaria PascalCase; mesmo bug já corrigido uma vez neste repo, ver company/dto.go).
type Response struct {
	ID                        string          `json:"id"`
	Title                     string          `json:"title"`
	Status                    string          `json:"status"`
	Location                  string          `json:"location"`
	Meta                      string          `json:"meta"`
	TotalCandidates           int             `json:"totalCandidates"`
	CurrentPhaseLabel         string          `json:"currentPhaseLabel"`
	CurrentPhaseKey           string          `json:"currentPhaseKey"`
	FunnelPercent             int             `json:"funnelPercent"`
	Phases                    []PhaseResponse `json:"phases"`
	AcceptsPublicApplications bool            `json:"acceptsPublicApplications"`
}

func toResponse(v *CampaignView) *Response {
	phases := make([]PhaseResponse, len(v.Phases))
	for i, pc := range v.Phases {
		phases[i] = PhaseResponse{Key: pc.Key, Num: i + 1, Label: phaseLabels[pc.Key], Count: pc.Count}
	}
	return &Response{
		ID:                        v.ID,
		Title:                     v.Title,
		Status:                    string(v.Status),
		Location:                  buildLocation(v.Campaign),
		Meta:                      buildMeta(*v),
		TotalCandidates:           v.TotalCandidates,
		CurrentPhaseLabel:         phaseLabels[v.CurrentPhaseKey],
		CurrentPhaseKey:           v.CurrentPhaseKey,
		FunnelPercent:             v.FunnelPercent,
		Phases:                    phases,
		AcceptsPublicApplications: v.AcceptsPublicApplications,
	}
}

// SetPublicLinkRequest é o payload de POST /campaigns/:id/public-application-link — estado
// desejado explícito ({enabled: true|false}), não um toggle cego.
type SetPublicLinkRequest struct {
	Enabled bool `json:"enabled"`
}

// PublicInfoResponse é o que GET /public/campaigns/:id devolve pro candidato anônimo — só o
// necessário pra mostrar a vaga, nunca nada interno (ver PublicInfo no model.go).
type PublicInfoResponse struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	CompanyName  string `json:"companyName"`
	Location     string `json:"location"`
	Modality     string `json:"modality"`
	ContractType string `json:"contractType"`
	Seniority    string `json:"seniority"`
}

func toPublicInfoResponse(v *PublicInfo) *PublicInfoResponse {
	return &PublicInfoResponse{
		ID:          v.ID,
		Title:       v.Title,
		CompanyName: v.CompanyName,
		Location: buildLocation(Campaign{
			City: v.City, State: v.State, Modality: v.Modality, ContractType: v.ContractType,
		}),
		Modality:     modalityLabels[v.Modality],
		ContractType: contractTypeLabels[v.ContractType],
		Seniority:    seniorityLabels[v.Seniority],
	}
}

// FunnelSummaryResponse é o payload de GET /reports/funnel-summary — mesma forma de PhaseResponse,
// mas agregada por empresa em vez de por campanha (sempre as 5 chaves fixas, mesmo em 0).
type FunnelSummaryResponse = PhaseResponse

// CampaignPerformanceResponse é uma linha de GET /reports/campaign-performance.
type CampaignPerformanceResponse struct {
	CampaignID        string  `json:"campaignId"`
	CampaignTitle     string  `json:"campaignTitle"`
	Status            string  `json:"status"`
	TotalCandidates   int     `json:"totalCandidates"`
	SelectedCount     int     `json:"selectedCount"`
	ConversionPct     float64 `json:"conversionPct"`
	CurrentPhaseLabel string  `json:"currentPhaseLabel"`
}

func toPerformanceResponse(row *PerformanceRow) *CampaignPerformanceResponse {
	return &CampaignPerformanceResponse{
		CampaignID:        row.ID,
		CampaignTitle:     row.Title,
		Status:            string(row.Status),
		TotalCandidates:   row.TotalCandidates,
		SelectedCount:     row.SelectedCount,
		ConversionPct:     conversionPct(row.SelectedCount, row.TotalCandidates),
		CurrentPhaseLabel: phaseLabels[row.CurrentPhaseKey],
	}
}

// --- Formatação pt-BR (apresentação — nunca decide estado, só formata o que o service calculou) ---

var phaseLabels = map[string]string{
	PhaseRecebidos:    "Recebidos",
	PhaseFit:          "Fit Cultural",
	PhaseTecnica:      "Triagem Técnica",
	PhaseEntrevista:   "Entrevista Estruturada",
	PhaseSelecionados: "Selecionados",
}

var modalityLabels = map[string]string{
	"remoto":     "Remoto",
	"hibrido":    "Híbrido",
	"presencial": "Presencial",
}

var contractTypeLabels = map[string]string{
	"clt":     "CLT",
	"pj":      "PJ",
	"estagio": "Estágio",
}

var seniorityLabels = map[string]string{
	"junior": "Júnior",
	"pleno":  "Pleno",
	"senior": "Sênior",
}

// buildLocation monta "{cidade}, {estado} · {modalidade} · {tipo de contrato}", omitindo o
// segmento cidade/estado quando a cidade está vazia (vagas remotas sem cidade fixa).
func buildLocation(c Campaign) string {
	segments := make([]string, 0, 3)
	if c.City != "" {
		if c.State != "" {
			segments = append(segments, fmt.Sprintf("%s, %s", c.City, c.State))
		} else {
			segments = append(segments, c.City)
		}
	}
	segments = append(segments, modalityLabels[c.Modality], contractTypeLabels[c.ContractType])
	return joinMiddleDot(segments)
}

// buildMeta monta o texto de status da campanha — "Aberta há N dias" / "Pausada há N dias" /
// "Encerrada em DD/MM/YYYY · N contratação(ões)".
func buildMeta(v CampaignView) string {
	switch v.Status {
	case StatusPaused:
		if v.PausedAt != nil {
			return fmt.Sprintf("Pausada há %d dias", daysSince(*v.PausedAt))
		}
		return "Pausada"
	case StatusClosed:
		if v.ClosedAt != nil {
			return fmt.Sprintf("Encerrada em %s · %s", v.ClosedAt.Format("02/01/2006"), hiresLabel(v.HiredCount))
		}
		return "Encerrada"
	default:
		return fmt.Sprintf("Aberta há %d dias", daysSince(v.OpenedAt))
	}
}

// daysSince conta dias corridos entre t e agora — usado em "Aberta/Pausada há N dias".
func daysSince(t time.Time) int {
	days := int(time.Since(t).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// conversionPct replica exatamente a fórmula do MockApiService do frontend: arredonda pra uma
// casa decimal (multiplica por 1000, arredonda pro inteiro mais próximo, divide por 10).
func conversionPct(selected, total int) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(float64(selected)/float64(total)*1000) / 10
}

func hiresLabel(count int) string {
	if count == 1 {
		return "1 contratação"
	}
	return fmt.Sprintf("%d contratações", count)
}

func joinMiddleDot(segments []string) string {
	out := ""
	for _, s := range segments {
		if s == "" {
			continue
		}
		if out != "" {
			out += " · "
		}
		out += s
	}
	return out
}
