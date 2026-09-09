package user

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
)

type Repository interface {
	FindByID(ctx context.Context, id string) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	CountActiveByCompany(ctx context.Context, companyID string) (int, error)
	Create(ctx context.Context, u *User) error
	SetActive(ctx context.Context, id string, active bool) error
}

type postgresRepository struct {
	db database.DB
}

func NewRepository(db database.DB) Repository {
	return &postgresRepository{db: db}
}

const userColumns = "id, company_id, name, email, password_hash, role, is_active, last_login_at, created_at, updated_at"

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.CompanyID, &u.Name, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (r *postgresRepository) FindByID(ctx context.Context, id string) (*User, error) {
	row := r.db.QueryRow(ctx, "select "+userColumns+" from users where id = $1", id)
	return scanUser(row)
}

func (r *postgresRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
	// `email` é citext no banco — a comparação já é case-insensitive sem esforço aqui.
	row := r.db.QueryRow(ctx, "select "+userColumns+" from users where email = $1", email)
	return scanUser(row)
}

func (r *postgresRepository) CountActiveByCompany(ctx context.Context, companyID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, "select count(*) from users where company_id = $1 and is_active", companyID).Scan(&count)
	return count, err
}

func (r *postgresRepository) Create(ctx context.Context, u *User) error {
	// O trigger trg_users_seat_limit no banco é quem garante o limite de
	// assentos de verdade — este insert só falha se ele disparar.
	row := r.db.QueryRow(ctx, `
		insert into users (company_id, name, email, password_hash, role)
		values ($1, $2, $3, $4, $5)
		returning id, created_at, updated_at
	`, u.CompanyID, u.Name, u.Email, u.PasswordHash, u.Role)
	return row.Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
}

func (r *postgresRepository) SetActive(ctx context.Context, id string, active bool) error {
	_, err := r.db.Exec(ctx, "update users set is_active = $2 where id = $1", id, active)
	return err
}
