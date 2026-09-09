package candidate

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	FindByID(ctx context.Context, companyID, id string) (*Candidate, error)
	ListByCampaign(ctx context.Context, companyID, campaignID string, phaseKey string) ([]Candidate, error)
	RecordDecision(ctx context.Context, companyID, id string, req DecideRequest, decidedByUserID string) error
}

type postgresRepository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) FindByID(ctx context.Context, companyID, id string) (*Candidate, error) {
	// TODO: select * from candidates where id = $1 and company_id = $2
	return nil, nil
}

func (r *postgresRepository) ListByCampaign(ctx context.Context, companyID, campaignID string, phaseKey string) ([]Candidate, error) {
	// TODO: select * from candidates where campaign_id = $1 and phase_key = $2
	return nil, nil
}

func (r *postgresRepository) RecordDecision(ctx context.Context, companyID, id string, req DecideRequest, decidedByUserID string) error {
	// TODO: numa transação — insert into candidate_decisions, update candidates
	// (phase_key/status), e se reprovação qualificada: upsert em talents
	return nil
}
