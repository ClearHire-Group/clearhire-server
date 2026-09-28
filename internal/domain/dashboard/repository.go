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

	// ReadyToAdvance devolve os candidatos aguardando decisão do RH cuja avaliação de IA MAIS
	// RECENTE, na fase em que estão agora, tem match_pct >= minMatch — maior match primeiro, o mais
	// antigo na frente em caso de empate. Só campanhas ativas: pausada/encerrada não pede ação.
	ReadyToAdvance(ctx context.Context, companyID string, minMatch, limit int) ([]ReadyCandidate, error)
	// WaitingByPhase agrupa os candidatos aguardando decisão por (campanha, fase) e devolve só os
	// grupos com pelo menos minCount candidatos, do maior pro menor.
	WaitingByPhase(ctx context.Context, companyID string, minCount, limit int) ([]WaitingGroup, error)
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

func (r *postgresRepository) ReadyToAdvance(ctx context.Context, companyID string, minMatch, limit int) ([]ReadyCandidate, error) {
	// company_id vem SEMPRE do usuário autenticado (CLAUDE.md): o join com campaigns repete
	// company_id de propósito, e o `where` filtra os dois lados. A avaliação é a mais recente DA
	// FASE ATUAL do candidato — uma nota de fase anterior não diz nada sobre a etapa em que ele
	// está; sem avaliação nessa fase, o candidato simplesmente não entra (join lateral interno).
	// A próxima fase é a de menor `position` acima da atual naquela campanha (funil configurável).
	rows, err := r.db.Query(ctx, `
		select c.id, c.name, c.campaign_id, camp.title, c.phase_key::text,
		       coalesce((
		         select nxt.phase_key::text
		         from campaign_phases nxt
		         where nxt.campaign_id = c.campaign_id and nxt.position > cp.position
		         order by nxt.position
		         limit 1
		       ), ''),
		       a.match_pct
		from candidates c
		join campaigns camp
		  on camp.id = c.campaign_id and camp.company_id = c.company_id
		join campaign_phases cp
		  on cp.campaign_id = c.campaign_id and cp.phase_key = c.phase_key
		join lateral (
		  select ai.match_pct
		  from candidate_ai_assessments ai
		  where ai.candidate_id = c.id and ai.phase_key = c.phase_key
		  order by ai.created_at desc
		  limit 1
		) a on true
		where c.company_id = $1 and c.deleted_at is null
		  and camp.deleted_at is null and camp.status = 'ativa'
		  and c.status = 'aguardando_decisao'
		  and a.match_pct >= $2
		order by a.match_pct desc, c.created_at asc, c.id
		limit $3
	`, companyID, minMatch, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ReadyCandidate, 0)
	for rows.Next() {
		var rc ReadyCandidate
		if err := rows.Scan(&rc.CandidateID, &rc.CandidateName, &rc.CampaignID, &rc.CampaignTitle,
			&rc.PhaseKey, &rc.NextPhaseKey, &rc.MatchPct); err != nil {
			return nil, err
		}
		out = append(out, rc)
	}
	return out, rows.Err()
}

func (r *postgresRepository) WaitingByPhase(ctx context.Context, companyID string, minCount, limit int) ([]WaitingGroup, error) {
	rows, err := r.db.Query(ctx, `
		select c.campaign_id, camp.title, c.phase_key::text, count(*)
		from candidates c
		join campaigns camp
		  on camp.id = c.campaign_id and camp.company_id = c.company_id
		where c.company_id = $1 and c.deleted_at is null
		  and camp.deleted_at is null and camp.status = 'ativa'
		  and c.status = 'aguardando_decisao'
		group by c.campaign_id, camp.title, c.phase_key
		having count(*) >= $2
		order by count(*) desc, camp.title, c.phase_key
		limit $3
	`, companyID, minCount, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]WaitingGroup, 0)
	for rows.Next() {
		var g WaitingGroup
		if err := rows.Scan(&g.CampaignID, &g.CampaignTitle, &g.PhaseKey, &g.Count); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
