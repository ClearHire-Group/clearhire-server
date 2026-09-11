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
	// ValuesByCompany devolve os valores organizacionais na ordem estável de exibição
	// (company_culture_values.position) — tabela filha, não coluna de companies.
	ValuesByCompany(ctx context.Context, companyID string) ([]string, error)
	UpdateCultureProfile(ctx context.Context, id string, tone, importanceNote string, values []string) error
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

func (r *postgresRepository) ValuesByCompany(ctx context.Context, companyID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		select value from company_culture_values
		where company_id = $1
		order by position
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

// UpdateCultureProfile grava tone/importanceNote na linha de companies e substitui
// company_culture_values inteira (delete + insert em lote) — perfil cultural é estado
// atual, sem histórico (nenhuma regra de negócio depende de versão anterior hoje).
func (r *postgresRepository) UpdateCultureProfile(ctx context.Context, id string, tone, importanceNote string, values []string) error {
	if _, err := r.db.Exec(ctx, `
		update companies set culture_tone = nullif($2, ''), culture_importance_note = nullif($3, '')
		where id = $1 and deleted_at is null
	`, id, tone, importanceNote); err != nil {
		return err
	}

	if _, err := r.db.Exec(ctx, `delete from company_culture_values where company_id = $1`, id); err != nil {
		return err
	}
	for i, v := range values {
		if _, err := r.db.Exec(ctx, `
			insert into company_culture_values (company_id, value, position)
			values ($1, $2, $3)
		`, id, v, i); err != nil {
			return err
		}
	}
	return nil
}
