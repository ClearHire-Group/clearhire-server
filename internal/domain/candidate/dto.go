package candidate

import (
	"fmt"
	"strings"
)

// initials devolve as duas letras que a bolinha de avatar mostra ("Marina Albuquerque" -> "MA").
func initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return ""
	}
	first := []rune(parts[0])[0]
	if len(parts) == 1 {
		return strings.ToUpper(string(first))
	}
	last := []rune(parts[len(parts)-1])[0]
	return strings.ToUpper(string(first) + string(last))
}

// avatarColorIndex escolhe entre as 3 cores de avatar de forma determinística a partir do id — o
// front hoje recalcula a cor pela posição na lista (ver candidate-view.ts, toCandidateViews), não
// por este campo, mas ele precisa existir e ser um 0|1|2 válido no contrato.
func avatarColorIndex(id string) int {
	sum := 0
	for _, r := range id {
		sum += int(r)
	}
	if sum < 0 {
		sum = -sum
	}
	return sum % 3
}

// DecideRequest é o payload de POST /candidates/:id/decisions — o clique
// manual de aprovar/reprovar (ver db/schema.sql, tabela candidate_decisions).
// Nunca automático: a IA sugere, o RH decide (invariante do produto).
type DecideRequest struct {
	Decision string `json:"decision" validate:"required,oneof=avancar reprovar"`
	// RejectionReasonKey só é obrigatório quando Decision = "reprovar" (checado no service, não
	// dá pra expressar "obrigatório condicional a outro campo" só com tags simples aqui) — a lista
	// fechada é a mesma Tabela 1 do documento, espelhada em rejection_reasons e no frontend
	// (REJECTION_REASONS).
	RejectionReasonKey *string `json:"rejectionReasonKey" validate:"omitempty,oneof=perdeu_outro_candidato senioridade_acima faltou_skill pretensao_acima_budget timing_indisponibilidade reprovacao_tecnica fit_cultural_incompativel"`
	SendBankInvite     bool    `json:"sendBankInvite"`
}

// phaseLabels espelha o mesmo mapa de internal/domain/campaign/dto.go — duplicado de propósito
// (candidate e campaign são domínios pares, sem import cruzado).
var phaseLabels = map[string]string{
	PhaseRecebidos:    "Recebidos",
	PhaseFit:          "Fit Cultural",
	PhaseTecnica:      "Triagem Técnica",
	PhaseEntrevista:   "Entrevista Estruturada",
	PhaseSelecionados: "Selecionados",
}

// phaseLabel cai de volta na própria chave quando a fase não está no mapa — o rótulo só alimenta
// texto de UI, então uma chave crua é degradação aceitável, melhor que string vazia.
func phaseLabel(key string) string {
	if label, ok := phaseLabels[key]; ok {
		return label
	}
	return key
}

// statusLabel traduz (status, fase) pro rótulo em português que a tela já sabe estilizar (ver
// clearhire-app core/candidate-view.ts, STATUS_STYLES — as strings aqui têm que bater exatamente
// com as chaves de lá, inclusive o "·" entre "Em análise" e o nome da fase).
func statusLabel(status Status, phaseKey string) string {
	switch status {
	case StatusAwaitingAI:
		return "Aguardando análise da IA"
	case StatusUnderReview:
		if label, ok := phaseLabels[phaseKey]; ok {
			return fmt.Sprintf("Em análise · %s", label)
		}
		return "Em análise"
	case StatusAwaitingDecision:
		return "Aguardando decisão"
	case StatusRejected:
		return "Reprovada"
	case StatusProposal:
		return "Proposta em elaboração"
	case StatusHired:
		return "Contratada"
	default:
		return string(status)
	}
}

// experienceLabel monta o texto livre que a tela já sabe exibir ("6 anos de experiência") — anos
// nil (candidato sem essa informação ainda) vira string vazia, nunca "0 anos" (0 é um valor real
// e diferente de "não informado").
func experienceLabel(years *int) string {
	if years == nil {
		return ""
	}
	if *years == 1 {
		return "1 ano de experiência"
	}
	return fmt.Sprintf("%d anos de experiência", *years)
}

func locationLabel(city, state string) string {
	if city == "" && state == "" {
		return ""
	}
	if city == "" || state == "" {
		return city + state
	}
	return fmt.Sprintf("%s, %s", city, state)
}

// CandidateResponse é uma linha de GET /campaigns/:id/candidates — o que a coluna de fase da tela
// de campanha lista. avatarColorIndex é derivado do id (determinístico: mesmo candidato sempre cai
// na mesma cor, sem precisar persistir isso em lugar nenhum).
type CandidateResponse struct {
	ID                 string  `json:"id"`
	CampaignID         string  `json:"campaignId"`
	Phase              string  `json:"phase"`
	Name               string  `json:"name"`
	Email              string  `json:"email"`
	Experience         string  `json:"experience"`
	Location           string  `json:"location"`
	MatchPct           *int    `json:"matchPct"`
	Status             string  `json:"status"`
	Initials           string  `json:"initials"`
	AvatarColorIndex   int     `json:"avatarColorIndex"`
	TalentID           *string `json:"talentId,omitempty"`
	RejectionReasonKey *string `json:"rejectionReasonKey,omitempty"`
}

func toCandidateResponse(c *Candidate) *CandidateResponse {
	return &CandidateResponse{
		ID:                 c.ID,
		CampaignID:         c.CampaignID,
		Phase:              c.PhaseKey,
		Name:               c.Name,
		Email:              c.Email,
		Experience:         experienceLabel(c.YearsExperience),
		Location:           locationLabel(c.City, c.State),
		MatchPct:           c.MatchPct,
		Status:             statusLabel(c.Status, c.PhaseKey),
		Initials:           initials(c.Name),
		AvatarColorIndex:   avatarColorIndex(c.ID),
		TalentID:           c.TalentID,
		RejectionReasonKey: c.RejectionReasonKey,
	}
}

// CandidateProfileResponse é o que GET /candidates/:id devolve — espelha CandidateProfileData do
// frontend (core/models.ts) campo a campo.
type CandidateProfileResponse struct {
	CandidateID     string                    `json:"candidateId"`
	Name            string                    `json:"name"`
	Initials        string                    `json:"initials"`
	Location        string                    `json:"location"`
	ExperienceLabel string                    `json:"experienceLabel"`
	PhaseLabel      string                    `json:"phaseLabel"`
	Accent          string                    `json:"accent"`
	Contact         candidateContact          `json:"contact"`
	Summary         string                    `json:"summary"`
	Experience      []experienceEntryResponse `json:"experience"`
	Education       candidateEducation        `json:"education"`
	Skills          []string                  `json:"skills"`
	AI              candidateAIResponse       `json:"ai"`
}

type candidateContact struct {
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	LinkedIn string `json:"linkedin"`
}

type candidateEducation struct {
	Degree      string `json:"degree"`
	Institution string `json:"institution"`
	Period      string `json:"period"`
}

type experienceEntryResponse struct {
	Role        string `json:"role"`
	Company     string `json:"company"`
	Period      string `json:"period"`
	Description string `json:"description"`
}

type candidateAIResponse struct {
	MatchPct      int      `json:"matchPct"`
	MatchLabel    string   `json:"matchLabel"`
	MatchNote     string   `json:"matchNote"`
	Strengths     []string `json:"strengths"`
	Concerns      []string `json:"concerns"`
	Justification string   `json:"justification"`
}

// DecideResponse é o que POST /candidates/:id/decisions devolve — só o que o frontend
// (candidate-profile.component.ts) de fato lê depois de uma decisão: a fase nova (avançar) ou o
// id do talento criado (reprovação qualificada com convite). Nunca a entidade inteira.
type DecideResponse struct {
	Phase    string  `json:"phase,omitempty"`
	TalentID *string `json:"talentId,omitempty"`
}

func toDecideResponse(r *DecideResult) *DecideResponse {
	return &DecideResponse{Phase: r.Phase, TalentID: r.TalentID}
}

// accentForPhase alterna o acento visual entre terracota/sand por fase — mesmo padrão usado nos
// dados mock originais (fases mais avançadas do funil puxam pro sand). Puramente decorativo.
func accentForPhase(phaseKey string) string {
	switch phaseKey {
	case PhaseEntrevista, PhaseSelecionados:
		return "sand"
	default:
		return "terracota"
	}
}

func toCandidateProfileResponse(d *CandidateDetail) *CandidateProfileResponse {
	experience := make([]experienceEntryResponse, len(d.Experience))
	for i, e := range d.Experience {
		experience[i] = experienceEntryResponse{Role: e.Role, Company: e.Company, Period: e.PeriodLabel, Description: e.Description}
	}

	ai := candidateAIResponse{Strengths: []string{}, Concerns: []string{}}
	if d.AI != nil {
		ai = candidateAIResponse{
			MatchPct:      d.AI.MatchPct,
			MatchLabel:    d.AI.MatchLabel,
			MatchNote:     d.AI.MatchNote,
			Strengths:     d.AI.Strengths,
			Concerns:      d.AI.Concerns,
			Justification: d.AI.Justification,
		}
	}

	return &CandidateProfileResponse{
		CandidateID:     d.ID,
		Name:            d.Name,
		Initials:        initials(d.Name),
		Location:        locationLabel(d.City, d.State),
		ExperienceLabel: experienceLabel(d.YearsExperience),
		PhaseLabel:      phaseLabels[d.PhaseKey],
		Accent:          accentForPhase(d.PhaseKey),
		Contact:         candidateContact{Email: d.Email, Phone: d.Phone, LinkedIn: d.LinkedInURL},
		Summary:         d.Summary,
		Experience:      experience,
		Education:       candidateEducation{Degree: d.EducationDegree, Institution: d.EducationInstitution, Period: d.EducationPeriod},
		Skills:          d.Skills,
		AI:              ai,
	}
}
