package talent

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// RegisterManualRequest é o payload de POST /talents — cadastro manual, fora de campanha (LinkedIn,
// evento, indicação). A validação (nome, tamanhos) é do domínio candidate, que devolve o erro por campo.
type RegisterManualRequest struct {
	Name           string `json:"name"`
	RawProfileText string `json:"rawProfileText"`
	ContextNote    string `json:"contextNote"`
}

// TalentResponse espelha Talent de clearhire-app/src/app/core/models.ts campo a campo. Os rótulos já
// saem prontos (mesmo formato do mock do front), e "A confirmar" é o valor que o front usa para
// "não informado" ao calcular a completude do perfil (talent-view.ts).
type TalentResponse struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Initials         string               `json:"initials"`
	AvatarColorIndex int                  `json:"avatarColorIndex"`
	Location         string               `json:"location"`
	Modality         string               `json:"modality"`
	Seniority        string               `json:"seniority"`
	YearsExperience  int                  `json:"yearsExperience"`
	Sectors          []string             `json:"sectors"`
	Skills           []skillResponse      `json:"skills"`
	Languages        []string             `json:"languages"`
	SalaryRangeLabel string               `json:"salaryRangeLabel"`
	AvailabilityLbl  string               `json:"availabilityLabel"`
	Origin           string               `json:"origin"`
	LegalBasis       string               `json:"legalBasis"`
	ConsentState     string               `json:"consentState"`
	ConsentDateLabel string               `json:"consentDateLabel,omitempty"`
	UpdatedAt        string               `json:"updatedAt"`
	Summary          string               `json:"summary"`
	Experience       []experienceResponse `json:"experience"`
	Education        educationResponse    `json:"education"`
	RecruiterNotes   string               `json:"recruiterNotes,omitempty"`
	History          []historyResponse    `json:"history"`
}

type skillResponse struct {
	Term            string `json:"term"`
	Level           string `json:"level,omitempty"`
	YearsExperience *int   `json:"yearsExperience,omitempty"`
}

type experienceResponse struct {
	Role        string `json:"role"`
	Company     string `json:"company"`
	Period      string `json:"period"`
	Description string `json:"description"`
}

type educationResponse struct {
	Degree      string `json:"degree"`
	Institution string `json:"institution"`
	Period      string `json:"period"`
}

type historyResponse struct {
	CampaignID         string  `json:"campaignId"`
	CampaignTitle      string  `json:"campaignTitle"`
	ReachedPhaseLabel  string  `json:"reachedPhaseLabel"`
	OutcomeLabel       string  `json:"outcomeLabel"`
	RejectionReasonKey *string `json:"rejectionReasonKey,omitempty"`
}

type coverageResponse struct {
	SkillTerm string `json:"skillTerm"`
	Count     int    `json:"count"`
}

const unknown = "A confirmar"

// Rótulos duplicados de propósito dos de candidate/campaign — domínios pares, sem import cruzado.
var phaseLabels = map[string]string{
	"recebidos": "Recebidos", "fit": "Fit Cultural", "tecnica": "Triagem Técnica",
	"entrevista": "Entrevista Estruturada", "selecionados": "Selecionados",
}

var rejectionLabels = map[string]string{
	"perdeu_outro_candidato":    "perdeu para outro candidato",
	"senioridade_acima":         "senioridade acima da vaga",
	"faltou_skill":              "faltou skill específica",
	"pretensao_acima_budget":    "pretensão acima do budget",
	"timing_indisponibilidade":  "indisponível no momento",
	"reprovacao_tecnica":        "reprovação técnica",
	"fit_cultural_incompativel": "fit cultural incompatível",
}

var modalityLabels = map[string]string{"remoto": "Remoto", "hibrido": "Híbrido", "presencial": "Presencial"}
var seniorityLabels = map[string]string{"junior": "Júnior", "pleno": "Pleno", "senior": "Sênior"}

var months = []string{"jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"}

func initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return ""
	}
	first := []rune(parts[0])[0]
	if len(parts) == 1 {
		return strings.ToUpper(string(first))
	}
	return strings.ToUpper(string(first) + string([]rune(parts[len(parts)-1])[0]))
}

func avatarColorIndex(id string) int {
	sum := 0
	for _, r := range id {
		sum += int(r)
	}
	return sum % 3
}

func labelOr(labels map[string]string, value string) string {
	if value == "" {
		return unknown
	}
	if label, ok := labels[strings.ToLower(value)]; ok {
		return label
	}
	return value
}

func location(city, state string) string {
	switch {
	case city != "" && state != "":
		return city + ", " + state
	case city != "" || state != "":
		return city + state
	default:
		return unknown
	}
}

// salaryK formata como o mock: "R$ 18k – 22k". Abaixo de mil, o valor cheio.
func salaryK(v float64) string {
	if v >= 1000 {
		k := v / 1000
		if k == math.Trunc(k) {
			return fmt.Sprintf("%.0fk", k)
		}
		return strings.Replace(fmt.Sprintf("%.1fk", k), ".", ",", 1)
	}
	return fmt.Sprintf("%.0f", v)
}

func salaryRange(min, max *float64) string {
	switch {
	case min != nil && max != nil:
		return fmt.Sprintf("R$ %s – %s", salaryK(*min), salaryK(*max))
	case min != nil:
		return fmt.Sprintf("A partir de R$ %s", salaryK(*min))
	case max != nil:
		return fmt.Sprintf("Até R$ %s", salaryK(*max))
	default:
		return unknown
	}
}

func availability(from *time.Time, note string, now time.Time) string {
	if from != nil {
		if !from.After(now) {
			return "Disponível imediatamente"
		}
		return fmt.Sprintf("Disponível a partir de %s/%d", months[from.Month()-1], from.Year())
	}
	if note != "" {
		return note
	}
	return unknown
}

func outcome(status string, reason *string) string {
	switch status {
	case "reprovado":
		if reason != nil {
			if label, ok := rejectionLabels[*reason]; ok {
				return "Reprovado — " + label
			}
		}
		return "Reprovado"
	case "proposta":
		return "Aprovado — proposta em elaboração"
	case "contratado":
		return "Contratado"
	default:
		return "Em andamento"
	}
}

// ReverseMatchRequest é o payload de POST /campaigns/reverse-match — bate 1:1 com o que o frontend
// já manda (clearhire-app core/data-api.ts, getReverseMatchForNewCampaign): título é o único campo
// obrigatório (pode ser vaga em rascunho, sem campanha persistida ainda); modalidade, senioridade e
// requisitos são opcionais. Requirements existe porque título sozinho raramente nomeia skill (ver
// comentário de MatchCriteria em reversematch.go).
type ReverseMatchRequest struct {
	Title        string `json:"title" validate:"required,max=200"`
	Modality     string `json:"modality" validate:"omitempty,oneof=remoto hibrido presencial"`
	Seniority    string `json:"seniority" validate:"omitempty,oneof=junior pleno senior"`
	Requirements string `json:"requirements" validate:"omitempty,max=5000"`
}

// TalentMatchResponse espelha TalentMatch do frontend (core/models.ts) — o talento no MESMO
// formato de GET /talents (nunca um segundo shape pro mesmo dado), mais o resultado desta rodada
// de match especificamente.
type TalentMatchResponse struct {
	Talent    *TalentResponse      `json:"talent"`
	MatchPct  int                  `json:"matchPct"`
	Breakdown []ScoreBreakdownLine `json:"breakdown"`
}

func toMatchResponse(m *TalentMatch, now time.Time) *TalentMatchResponse {
	breakdown := m.Breakdown
	if breakdown == nil {
		breakdown = []ScoreBreakdownLine{}
	}
	return &TalentMatchResponse{Talent: toResponse(&m.Talent, now), MatchPct: m.MatchPct, Breakdown: breakdown}
}

// AssessTalentsRequest é o payload de POST /campaigns/:id/talent-recommendations/assess — etapa 2
// do match reverso. Teto de 5 no validate (espelha maxAssessedTalentsPerRequest em service.go; o
// service reforça o mesmo teto, então um validate desatualizado nunca abriria brecha, só uma
// mensagem de erro pior).
type AssessTalentsRequest struct {
	TalentIDs []string `json:"talentIds" validate:"required,min=1,max=5,unique,dive,uuid"`
}

// TalentAssessmentResponse espelha CandidateAssessment do frontend (core/models.ts), sem
// stageInsight/comparisonFlag: recomendação de banco não tem fase nem avaliação anterior a
// comparar — só StageFocus/PriorStageSummary vazios do lado do prompt (ver AssessTalentForCampaign).
type TalentAssessmentResponse struct {
	MatchPct           int      `json:"matchPct"`
	MatchLabel         string   `json:"matchLabel"`
	MatchNote          string   `json:"matchNote"`
	Strengths          []string `json:"strengths"`
	Concerns           []string `json:"concerns"`
	Justification      string   `json:"justification"`
	Confidence         string   `json:"confidence"`
	MissingInformation []string `json:"missingInformation"`
}

type TalentRecommendationResponse struct {
	Talent     *TalentResponse          `json:"talent"`
	Assessment TalentAssessmentResponse `json:"assessment"`
}

func toRecommendationResponse(r *TalentRecommendation, now time.Time) *TalentRecommendationResponse {
	a := r.Assessment
	strengths, concerns, missing := a.Strengths, a.Concerns, a.MissingInformation
	if strengths == nil {
		strengths = []string{}
	}
	if concerns == nil {
		concerns = []string{}
	}
	if missing == nil {
		missing = []string{}
	}
	return &TalentRecommendationResponse{
		Talent: toResponse(&r.Talent, now),
		Assessment: TalentAssessmentResponse{
			MatchPct: a.MatchPct, MatchLabel: a.MatchLabel, MatchNote: a.MatchNote,
			Strengths: strengths, Concerns: concerns, Justification: a.Justification,
			Confidence: a.Confidence, MissingInformation: missing,
		},
	}
}

func toResponse(t *Talent, now time.Time) *TalentResponse {
	years := 0
	if t.YearsExperience != nil {
		years = *t.YearsExperience
	}
	skills := make([]skillResponse, len(t.Skills))
	for i, s := range t.Skills {
		skills[i] = skillResponse{Term: s.Term, Level: s.Level, YearsExperience: s.YearsExperience}
	}
	languages := make([]string, len(t.Languages))
	for i, l := range t.Languages {
		languages[i] = strings.TrimSpace(l.Name + " " + l.Proficiency)
	}
	experience := make([]experienceResponse, len(t.Experience))
	for i, e := range t.Experience {
		experience[i] = experienceResponse{Role: e.Role, Company: e.Company, Period: e.PeriodLabel, Description: e.Description}
	}
	history := make([]historyResponse, len(t.History))
	for i, h := range t.History {
		phase := phaseLabels[h.PhaseKey]
		if phase == "" {
			phase = h.PhaseKey
		}
		history[i] = historyResponse{CampaignID: h.CampaignID, CampaignTitle: h.CampaignTitle, ReachedPhaseLabel: phase,
			OutcomeLabel: outcome(h.Status, h.RejectionReasonKey), RejectionReasonKey: h.RejectionReasonKey}
	}
	consentDate := ""
	if t.ConsentState == "consentido" && t.ConsentDate != nil {
		consentDate = "Consentiu em " + t.ConsentDate.Format("02/01/2006")
	}
	return &TalentResponse{
		ID: t.ID, Name: t.Name, Initials: initials(t.Name), AvatarColorIndex: avatarColorIndex(t.ID),
		Location: location(t.City, t.State), Modality: labelOr(modalityLabels, t.Modality),
		Seniority: labelOr(seniorityLabels, t.Seniority), YearsExperience: years,
		Sectors: t.Sectors, Skills: skills, Languages: languages,
		SalaryRangeLabel: salaryRange(t.SalaryMin, t.SalaryMax),
		AvailabilityLbl:  availability(t.AvailableFrom, t.AvailabilityNote, now),
		Origin:           t.Origin, LegalBasis: t.LegalBasis, ConsentState: t.ConsentState, ConsentDateLabel: consentDate,
		UpdatedAt: t.ProfileReviewedAt.UTC().Format(time.RFC3339), Summary: t.Summary, Experience: experience,
		Education:      educationResponse{Degree: t.EducationDegree, Institution: t.EducationInstitution, Period: t.EducationPeriod},
		RecruiterNotes: t.RecruiterNotes, History: history,
	}
}
