package campaign

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	FindByID(ctx context.Context, companyID, id string) (*Campaign, error)
	ListByCompany(ctx context.Context, companyID string) ([]Campaign, error)
	Create(ctx context.Context, c *Campaign, phases []Phase) error
	SetStatus(ctx context.Context, companyID, id string, status Status) error
}

type postgresRepository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) FindByID(ctx context.Context, companyID, id string) (*Campaign, error) {
	// TODO: select * from campaigns where id = $1 and company_id = $2 and deleted_at is null
	return nil, nil
}

func (r *postgresRepository) ListByCompany(ctx context.Context, companyID string) ([]Campaign, error) {
	// TODO: select * from campaigns where company_id = $1 and deleted_at is null
	return nil, nil
}

func (r *postgresRepository) Create(ctx context.Context, c *Campaign, phases []Phase) error {
	// TODO: insert into campaigns + campaign_phases numa transação
	return nil
}

func (r *postgresRepository) SetStatus(ctx context.Context, companyID, id string, status Status) error {
	// TODO: update campaigns set status = $3 where id = $1 and company_id = $2
	return nil
}
