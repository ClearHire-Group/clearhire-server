package company

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	FindByID(ctx context.Context, id string) (*Company, error)
	Create(ctx context.Context, c *Company) error
	UpdateCultureProfile(ctx context.Context, id string, req UpdateCultureProfileRequest) error
}

type postgresRepository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) FindByID(ctx context.Context, id string) (*Company, error) {
	// TODO: select * from companies where id = $1 and deleted_at is null
	return nil, nil
}

func (r *postgresRepository) Create(ctx context.Context, c *Company) error {
	// TODO: insert into companies
	return nil
}

func (r *postgresRepository) UpdateCultureProfile(ctx context.Context, id string, req UpdateCultureProfileRequest) error {
	// TODO: update companies set culture_tone = ..., culture_importance_note = ...
	// + upsert em company_culture_values
	return nil
}
