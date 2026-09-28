package dashboard

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
)

const (
	// periodDays é a janela de "Contratações no período" — bate com o texto fixo "Últimos 30
	// dias" que o cabeçalho do Dashboard já mostra (clearhire-app dashboard.component.html).
	periodDays = 30
	// trendPoints/trendSpacing: "5-6 pontos" pedido pelo sparkline (core/models.ts, comentário em
	// DashboardMetrics.activeCampaignsTrend). 6 pontos * 5 dias = 25 dias de janela, cabendo
	// dentro do período de 30 dias acima.
	trendPoints  = 6
	trendSpacing = 5 * 24 * time.Hour
)

// Regras do card "Pendências de revisão" (GetSuggestions). São constantes de propósito: a decisão
// de produto é "o que merece destaque", e mudar o número não deve exigir tocar em query nenhuma.
const (
	// readyMatchThreshold é o match_pct mínimo (da avaliação mais recente da IA, na fase atual) pra
	// um candidato aguardando decisão aparecer como "pode avançar". Não usar match_label pra isso:
	// é texto livre escrito pelo LLM ("Excelente match", "Bom fit", ...), bom pra exibir, ruim
	// como regra.
	readyMatchThreshold = 80
	// maxReadySuggestions/maxWaitingSuggestions limitam o card: ele é um resumo, a lista completa
	// mora na tela da campanha.
	maxReadySuggestions   = 3
	maxWaitingSuggestions = 2
	// minWaitingGroupSize: abaixo disso "N candidatos aguardam" seria só a mesma pessoa que a
	// sugestão individual já mostra.
	minWaitingGroupSize = 2
)

type Service interface {
	GetMetrics(ctx context.Context, companyID string) (*Metrics, error)
	GetSuggestions(ctx context.Context, companyID string) ([]Suggestion, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) GetMetrics(ctx context.Context, companyID string) (*Metrics, error) {
	refDays := referenceDays(time.Now(), trendPoints, trendSpacing)

	activeCampaigns, err := s.repo.ActiveCampaigns(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular campanhas ativas")
	}
	activeCampaignsTrend, err := s.repo.ActiveCampaignsTrend(ctx, companyID, refDays)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular tendência de campanhas ativas")
	}

	candidatesInProcess, candidatesCampaigns, err := s.repo.CandidatesInProcess(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular candidatos em processo")
	}

	hiresInPeriod, err := s.repo.HiresInPeriod(ctx, companyID, time.Now().AddDate(0, 0, -periodDays))
	if err != nil {
		return nil, apperror.Internal("falha ao calcular contratações do período")
	}

	hiresGoal, err := s.repo.HiringGoal(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar meta de contratações")
	}

	avgFunnelDaysRaw, err := s.repo.AvgFunnelDays(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular tempo médio no funil")
	}
	avgFunnelDaysTrendRaw, err := s.repo.AvgFunnelDaysTrend(ctx, companyID, refDays)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular tendência do tempo no funil")
	}
	avgFunnelDaysTrend := roundAll(avgFunnelDaysTrendRaw)

	return &Metrics{
		ActiveCampaigns:                 activeCampaigns,
		ActiveCampaignsTrendLabel:       countTrendLabel(activeCampaignsTrend, "esta semana"),
		ActiveCampaignsTrend:            activeCampaignsTrend,
		CandidatesInProcess:             candidatesInProcess,
		CandidatesInProcessContextLabel: campaignsLabel(candidatesCampaigns),
		HiresInPeriod:                   hiresInPeriod,
		HiresGoal:                       hiresGoal,
		AvgFunnelDays:                   round(avgFunnelDaysRaw),
		AvgFunnelDaysTrendLabel:         daysTrendLabel(avgFunnelDaysTrend),
		AvgFunnelDaysTrend:              avgFunnelDaysTrend,
	}, nil
}

// referenceDays gera `points` marcos no tempo, `spacing` afastados entre si, do mais antigo pro
// mais recente — o último é sempre `now` exato, pra o headline (valor "de agora") e o último ponto
// do trend nunca divergirem por causa de um corte de dia diferente.
func referenceDays(now time.Time, points int, spacing time.Duration) []time.Time {
	days := make([]time.Time, points)
	for i := range days {
		days[i] = now.Add(-time.Duration(points-1-i) * spacing)
	}
	return days
}

func round(v float64) int {
	return int(math.Round(v))
}

func roundAll(values []float64) []int {
	out := make([]int, len(values))
	for i, v := range values {
		out[i] = round(v)
	}
	return out
}

// countTrendLabel/daysTrendLabel comparam só a ponta (primeiro vs último ponto do trend) — mesmo
// padrão que MOCK_DASHBOARD_METRICS já usava no front (ex.: activeCampaignsTrendLabel: '+1 esta
// semana', avgFunnelDaysTrendLabel: '-3 dias'), agora computado em vez de hardcodado.
func countTrendLabel(trend []int, suffix string) string {
	if len(trend) < 2 {
		return "sem dados suficientes"
	}
	diff := trend[len(trend)-1] - trend[0]
	if diff == 0 {
		return "sem mudança " + suffix
	}
	sign := "+"
	if diff < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%d %s", sign, diff, suffix)
}

func daysTrendLabel(trend []int) string {
	if len(trend) < 2 {
		return "sem dados suficientes"
	}
	diff := trend[len(trend)-1] - trend[0]
	if diff == 0 {
		return "sem mudança"
	}
	sign := "+"
	if diff < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%d dias", sign, diff)
}

func campaignsLabel(n int) string {
	if n == 1 {
		return "1 campanha"
	}
	return fmt.Sprintf("%d campanhas", n)
}

// phaseLabels espelha os rótulos de campaign/dto.go e candidate/dto.go — duplicado de propósito
// (domínios pares não se importam; ver o mesmo comentário em candidate/model.go).
var phaseLabels = map[string]string{
	"recebidos":    "Recebidos",
	"fit":          "Fit Cultural",
	"tecnica":      "Triagem Técnica",
	"entrevista":   "Entrevista Estruturada",
	"selecionados": "Selecionados",
}

func phaseLabel(key string) string {
	if label, ok := phaseLabels[key]; ok {
		return label
	}
	return key
}

// GetSuggestions monta o card "Pendências de revisão" SEM LLM: duas consultas ao banco e texto
// montado aqui. A IA já fez o trabalho caro quando avaliou o candidato (candidate_ai_assessments);
// o card só filtra, ordena e conta em cima disso. Continua valendo "a IA sugere, o RH decide" —
// nada aqui muda estado, só aponta pra onde o RH deveria olhar.
//
// Ordem: candidatos prontos (o primeiro vem destacado) e depois os grupos de candidatos
// aguardando. Um candidato pode aparecer nos dois — no individual (pelo match) e no grupo (pela
// contagem); os dois respondem perguntas diferentes ("quem ver primeiro" × "quanto está parado").
// Devolve slice vazio, nunca nil, pra o JSON sair `[]`.
func (s *service) GetSuggestions(ctx context.Context, companyID string) ([]Suggestion, error) {
	ready, err := s.repo.ReadyToAdvance(ctx, companyID, readyMatchThreshold, maxReadySuggestions)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar candidatos prontos para avançar")
	}
	waiting, err := s.repo.WaitingByPhase(ctx, companyID, minWaitingGroupSize, maxWaitingSuggestions)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar candidatos aguardando decisão")
	}

	out := make([]Suggestion, 0, len(ready)+len(waiting))
	for i, rc := range ready {
		out = append(out, Suggestion{
			ID:                 "ready-" + rc.CandidateID,
			Message:            readyMessage(rc),
			PrimaryActionLabel: "Revisar candidatura",
			PrimaryActionRoute: []string{"/campanhas", rc.CampaignID, "candidatos", rc.CandidateID},
			Highlighted:        i == 0,
		})
	}
	for _, g := range waiting {
		out = append(out, Suggestion{
			ID:                 "waiting-" + g.CampaignID + "-" + g.PhaseKey,
			Message:            waitingMessage(g),
			PrimaryActionLabel: "Ver candidatos",
			PrimaryActionRoute: []string{"/campanhas", g.CampaignID, "candidatos"},
		})
	}
	return out, nil
}

// readyMessage evita concordância de gênero ("pronta"/"pronto") de propósito: o sistema não sabe
// nem deve inferir o gênero de quem se candidatou.
func readyMessage(rc ReadyCandidate) string {
	if rc.NextPhaseKey == "" {
		return fmt.Sprintf("%s (%d%% de match) aguarda sua decisão em %s na campanha %s.",
			rc.CandidateName, rc.MatchPct, phaseLabel(rc.PhaseKey), rc.CampaignTitle)
	}
	return fmt.Sprintf("%s (%d%% de match) pode avançar de %s para %s em %s.",
		rc.CandidateName, rc.MatchPct, phaseLabel(rc.PhaseKey), phaseLabel(rc.NextPhaseKey), rc.CampaignTitle)
}

func waitingMessage(g WaitingGroup) string {
	if g.Count == 1 {
		return fmt.Sprintf("1 candidato analisado em %s para %s aguarda sua decisão.",
			phaseLabel(g.PhaseKey), g.CampaignTitle)
	}
	return fmt.Sprintf("%d candidatos analisados em %s para %s aguardam sua decisão.",
		g.Count, phaseLabel(g.PhaseKey), g.CampaignTitle)
}
