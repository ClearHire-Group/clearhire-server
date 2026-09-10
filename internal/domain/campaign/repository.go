package campaign

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
)

type Repository interface {
	FindByID(ctx context.Context, companyID, id string) (*Campaign, error)
	ListByCompany(ctx context.Context, companyID string) ([]Campaign, error)
	Create(ctx context.Context, c *Campaign, phases []Phase) error
	SetStatus(ctx context.Context, companyID, id string, status Status, pausedAt *time.Time) error
	// PhaseCountsByCampaign/PhaseCountsByCompany devolvem a contagem CUMULATIVA por fase —
	// candidatos cuja fase atual está nesta posição do funil ou adiante, nunca uma contagem
	// exata "quem está sentado aqui agora". Ver PhaseCount no model.go pro porquê.
	PhaseCountsByCampaign(ctx context.Context, companyID, campaignID string) ([]PhaseCount, error)
	PhaseCountsByCompany(ctx context.Context, companyID string) ([]PhaseCount, error)
	// HiredCount só é relevante pra campanhas encerradas (usado no texto de `meta`) — ninguém
	// ainda produz esse estado nesta versão, então é sempre chamado sob demanda, não em massa.
	HiredCount(ctx context.Context, companyID, campaignID string) (int, error)
}

type postgresRepository struct {
	db database.DB
}

func NewRepository(db database.DB) Repository {
	return &postgresRepository{db: db}
}

const campaignColumns = `id, company_id, created_by_user_id, title, coalesce(city, ''), coalesce(state, ''),
	modality, contract_type, seniority, status, opened_at, paused_at, closed_at, created_at, updated_at`

func scanCampaign(row pgx.Row) (*Campaign, error) {
	var c Campaign
	err := row.Scan(
		&c.ID, &c.CompanyID, &c.CreatedByUserID, &c.Title, &c.City, &c.State,
		&c.Modality, &c.ContractType, &c.Seniority, &c.Status, &c.OpenedAt, &c.PausedAt, &c.ClosedAt,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *postgresRepository) FindByID(ctx context.Context, companyID, id string) (*Campaign, error) {
	row := r.db.QueryRow(ctx, `
		select `+campaignColumns+`
		from campaigns where id = $1 and company_id = $2 and deleted_at is null
	`, id, companyID)
	return scanCampaign(row)
}

func (r *postgresRepository) ListByCompany(ctx context.Context, companyID string) ([]Campaign, error) {
	rows, err := r.db.Query(ctx, `
		select `+campaignColumns+`
		from campaigns where company_id = $1 and deleted_at is null
		order by created_at desc
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var campaigns []Campaign
	for rows.Next() {
		c, err := scanCampaign(rows)
		if err != nil {
			return nil, err
		}
		campaigns = append(campaigns, *c)
	}
	return campaigns, rows.Err()
}

func (r *postgresRepository) Create(ctx context.Context, c *Campaign, phases []Phase) error {
	row := r.db.QueryRow(ctx, `
		insert into campaigns (company_id, created_by_user_id, title, city, state, modality, contract_type, seniority)
		values ($1, $2, $3, nullif($4, ''), nullif($5, ''), $6, $7, $8)
		returning id, status, opened_at, created_at, updated_at
	`, c.CompanyID, c.CreatedByUserID, c.Title, c.City, c.State, c.Modality, c.ContractType, c.Seniority)
	if err := row.Scan(&c.ID, &c.Status, &c.OpenedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return err
	}

	for _, p := range phases {
		if _, err := r.db.Exec(ctx, `
			insert into campaign_phases (campaign_id, phase_key, position)
			values ($1, $2, $3)
		`, c.ID, p.Key, p.Position); err != nil {
			return err
		}
	}
	return nil
}

func (r *postgresRepository) SetStatus(ctx context.Context, companyID, id string, status Status, pausedAt *time.Time) error {
	_, err := r.db.Exec(ctx, `
		update campaigns set status = $3, paused_at = $4
		where id = $1 and company_id = $2 and deleted_at is null
	`, id, companyID, status, pausedAt)
	return err
}

// PhaseCountsByCampaign/PhaseCountsByCompany compartilham a mesma forma de query: um self-join
// de campaign_phases contra ela mesma em `position >= `, seguido de left join com candidates (o
// left join é o que garante que uma fase sem candidato nenhum aparece como 0 em vez de sumir da
// lista — essencial enquanto a tabela candidates estiver vazia, como é hoje). A comparação por
// POSIÇÃO configurada é obrigatória, não a ordem do enum phase_key: a tela Nova Campanha deixa
// reordenar os módulos opcionais (fit/tecnica/entrevista) em qualquer ordem por campanha.
const phaseCountsQuery = `
	select target.campaign_id, target.phase_key, target.position, count(distinct c.id)
	from campaign_phases target
	join campaigns camp
	  on camp.id = target.campaign_id and camp.company_id = $1 and camp.deleted_at is null
	join campaign_phases own_cp
	  on own_cp.campaign_id = target.campaign_id and own_cp.position >= target.position
	left join candidates c
	  on c.campaign_id = own_cp.campaign_id and c.phase_key = own_cp.phase_key and c.deleted_at is null`

func (r *postgresRepository) queryPhaseCounts(ctx context.Context, sql string, args ...any) ([]PhaseCount, error) {
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var counts []PhaseCount
	for rows.Next() {
		var pc PhaseCount
		if err := rows.Scan(&pc.CampaignID, &pc.Key, &pc.Position, &pc.Count); err != nil {
			return nil, err
		}
		counts = append(counts, pc)
	}
	return counts, rows.Err()
}

func (r *postgresRepository) PhaseCountsByCampaign(ctx context.Context, companyID, campaignID string) ([]PhaseCount, error) {
	return r.queryPhaseCounts(ctx, phaseCountsQuery+`
		where target.campaign_id = $2
		group by target.campaign_id, target.phase_key, target.position
		order by target.position
	`, companyID, campaignID)
}

func (r *postgresRepository) PhaseCountsByCompany(ctx context.Context, companyID string) ([]PhaseCount, error) {
	return r.queryPhaseCounts(ctx, phaseCountsQuery+`
		group by target.campaign_id, target.phase_key, target.position
		order by target.campaign_id, target.position
	`, companyID)
}

func (r *postgresRepository) HiredCount(ctx context.Context, companyID, campaignID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		select count(*) from candidates
		where campaign_id = $1 and company_id = $2 and status = 'contratado' and deleted_at is null
	`, campaignID, companyID).Scan(&count)
	return count, err
}
