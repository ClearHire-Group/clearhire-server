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

type Service interface {
	GetMetrics(ctx context.Context, companyID string) (*Metrics, error)
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
