package company

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
)

type Repository interface {
	FindByID(ctx context.Context, id string) (*Company, error)
	Create(ctx context.Context, c *Company) error
	UpdateCultureProfile(ctx context.Context, id string, req UpdateCultureProfileRequest) error
}

type postgresRepository struct {
	db database.DB
}

func NewRepository(db database.DB) Repository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) FindByID(ctx context.Context, id string) (*Company, error) {
	// culture_tone/culture_importance_note são nuláveis (ninguém preencheu o
	// perfil cultural ainda) — coalesce pra string vazia em vez de exigir
	// *string em todo lugar que ler Company. Nenhuma regra de negócio depende
	// de distinguir "nunca preencheu" de "preencheu vazio" aqui.
	row := r.db.QueryRow(ctx, `
		select id, name, coalesce(culture_tone, ''), coalesce(culture_importance_note, ''), seat_limit, created_at, updated_at
		from companies where id = $1 and deleted_at is null
	`, id)
	var c Company
	err := row.Scan(&c.ID, &c.Name, &c.CultureTone, &c.CultureImportanceNote, &c.SeatLimit, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *postgresRepository) Create(ctx context.Context, c *Company) error {
	row := r.db.QueryRow(ctx, `
		insert into companies (name) values ($1)
		returning id, seat_limit, created_at, updated_at
	`, c.Name)
	return row.Scan(&c.ID, &c.SeatLimit, &c.CreatedAt, &c.UpdatedAt)
}

func (r *postgresRepository) UpdateCultureProfile(ctx context.Context, id string, req UpdateCultureProfileRequest) error {
	// TODO: update companies set culture_tone/culture_importance_note +
	// upsert em company_culture_values. Não faz parte do fluxo de auth.
	return nil
}
