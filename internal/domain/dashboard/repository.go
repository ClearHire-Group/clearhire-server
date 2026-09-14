package dashboard

import (
	"context"
	"time"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
)

type Repository interface {
	// ActiveCampaigns é a contagem EXATA de agora (status = 'ativa'), sem aproximação nenhuma.
	ActiveCampaigns(ctx context.Context, companyID string) (int, error)
	// CandidatesInProcess devolve a contagem de candidatos que ainda não chegaram a um estado
	// final (nem reprovado, nem contratado) e quantas campanhas distintas eles atravessam.
	CandidatesInProcess(ctx context.Context, companyID string) (count int, campaigns int, err error)
	// HiresInPeriod usa updated_at como proxy de "quando este candidato virou contratado" — não
	// existe hired_at dedicado no schema (status='contratado' ainda não tem nenhum caminho de
	// escrita real no domínio candidate, ver CLAUDE.md do repo; a query já fica correta pro dia
	// em que esse caminho existir, só ainda não há dado real que a exercite).
	HiresInPeriod(ctx context.Context, companyID string, since time.Time) (int, error)
	// AvgFunnelDays é a idade média (em dias) dos candidatos atualmente em processo — mesma
	// população de CandidatesInProcess, não "tempo até decisão" de quem já saiu do funil.
	AvgFunnelDays(ctx context.Context, companyID string) (float64, error)
	HiringGoal(ctx context.Context, companyID string) (int, error)

	// ActiveCampaignsTrend/AvgFunnelDaysTrend reconstroem o passado em cada `refDays[i]`, mais
	// antigo primeiro — aproximação via timestamps de ciclo de vida, não histórico registrado.
	// Uma query por ponto de propósito (N é pequeno, 5-6 pontos — ver core/models.ts) em vez de um
	// generate_series só: mais simples de ler e evita lidar com parâmetro de INTERVAL do pgx.
	ActiveCampaignsTrend(ctx context.Context, companyID string, refDays []time.Time) ([]int, error)
	AvgFunnelDaysTrend(ctx context.Context, companyID string, refDays []time.Time) ([]float64, error)
}

type postgresRepository struct {
	db database.DB
}

func NewRepository(db database.DB) Repository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) ActiveCampaigns(ctx context.Context, companyID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		select count(*) from campaigns
		where company_id = $1 and deleted_at is null and status = 'ativa'
	`, companyID).Scan(&count)
	return count, err
}

func (r *postgresRepository) CandidatesInProcess(ctx context.Context, companyID string) (int, int, error) {
	var count, campaigns int
	err := r.db.QueryRow(ctx, `
		select count(*), count(distinct campaign_id)
		from candidates
		where company_id = $1 and deleted_at is null
		  and status not in ('reprovado', 'contratado')
	`, companyID).Scan(&count, &campaigns)
	return count, campaigns, err
}

func (r *postgresRepository) HiresInPeriod(ctx context.Context, companyID string, since time.Time) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		select count(*) from candidates
		where company_id = $1 and deleted_at is null and status = 'contratado'
		  and updated_at >= $2
	`, companyID, since).Scan(&count)
	return count, err
}

func (r *postgresRepository) AvgFunnelDays(ctx context.Context, companyID string) (float64, error) {
	var days float64
	err := r.db.QueryRow(ctx, `
		select coalesce(avg(extract(epoch from (now() - created_at)) / 86400), 0)
		from candidates
		where company_id = $1 and deleted_at is null
		  and status not in ('reprovado', 'contratado')
	`, companyID).Scan(&days)
	return days, err
}

func (r *postgresRepository) HiringGoal(ctx context.Context, companyID string) (int, error) {
	var goal int
	err := r.db.QueryRow(ctx, `
		select hiring_goal_per_period from companies where id = $1
	`, companyID).Scan(&goal)
	return goal, err
}

func (r *postgresRepository) ActiveCampaignsTrend(ctx context.Context, companyID string, refDays []time.Time) ([]int, error) {
	values := make([]int, len(refDays))
	for i, day := range refDays {
		// "Ativa em `day`" aqui significa só "aberta e ainda não encerrada" (opened_at/closed_at)
		// — ignora pausa de propósito: não existe resumed_at pra reconstruir quando uma campanha
		// pausada voltou a ficar ativa, então o trend histórico não distingue pausada de ativa
		// (diferente do número "de agora" em ActiveCampaigns, que é exato). Pausar é evento raro
		// comparado a abrir/fechar, então a aproximação fica aceitável pro sparkline.
		if err := r.db.QueryRow(ctx, `
			select count(*) from campaigns
			where company_id = $1 and deleted_at is null
			  and opened_at <= $2
			  and (closed_at is null or closed_at > $2)
		`, companyID, day).Scan(&values[i]); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func (r *postgresRepository) AvgFunnelDaysTrend(ctx context.Context, companyID string, refDays []time.Time) ([]float64, error) {
	values := make([]float64, len(refDays))
	for i, day := range refDays {
		// "Ainda no funil em `day`" usa rejected_at como único sinal de saída — candidatos
		// contratados não têm timestamp de saída dedicado ainda (ver HiresInPeriod), então ficam
		// sempre excluídos por `status <> 'contratado'` mesmo reconstruindo o passado; aceitável
		// porque hoje não existe nenhum candidato contratado no sistema (ver comentário em
		// HiresInPeriod).
		if err := r.db.QueryRow(ctx, `
			select coalesce(avg(extract(epoch from ($2::timestamptz - created_at)) / 86400), 0)
			from candidates
			where company_id = $1 and deleted_at is null
			  and created_at <= $2
			  and (rejected_at is null or rejected_at > $2)
			  and status <> 'contratado'
		`, companyID, day).Scan(&values[i]); err != nil {
			return nil, err
		}
	}
	return values, nil
}
