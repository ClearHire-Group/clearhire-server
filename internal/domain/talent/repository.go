package talent

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	FindByID(ctx context.Context, companyID, id string) (*Talent, error)
	ListByCompany(ctx context.Context, companyID string) ([]Talent, error)
	Create(ctx context.Context, t *Talent) error
	Search(ctx context.Context, companyID string, query string) ([]Talent, error)
}

type postgresRepository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) FindByID(ctx context.Context, companyID, id string) (*Talent, error) {
	// TODO: select * from talents where id = $1 and company_id = $2 and deleted_at is null
	return nil, nil
}

func (r *postgresRepository) ListByCompany(ctx context.Context, companyID string) ([]Talent, error) {
	// TODO: select * from talents where company_id = $1 and consent_state <> 'oposicao_exclusao'
	return nil, nil
}

func (r *postgresRepository) Create(ctx context.Context, t *Talent) error {
	// TODO: insert into talents (cadastro manual) — LLM na escrita, seção 6.1 do doc
	return nil
}

func (r *postgresRepository) Search(ctx context.Context, companyID string, query string) ([]Talent, error) {
	// TODO: filtros estruturados (WHERE indexado) + pgvector pra parte semântica
	// — nunca LLM em runtime sobre o conjunto de perfis (seção 6.2 do doc)
	return nil, nil
}
