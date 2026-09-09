package user

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	FindByID(ctx context.Context, id string) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	CountActiveByCompany(ctx context.Context, companyID string) (int, error)
	Create(ctx context.Context, u *User) error
	SetActive(ctx context.Context, id string, active bool) error
}

type postgresRepository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) FindByID(ctx context.Context, id string) (*User, error) {
	// TODO: select * from users where id = $1
	return nil, nil
}

func (r *postgresRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
	// TODO: select * from users where email = $1 (citext, case-insensitive)
	return nil, nil
}

func (r *postgresRepository) CountActiveByCompany(ctx context.Context, companyID string) (int, error) {
	// TODO: select count(*) from users where company_id = $1 and is_active
	return 0, nil
}

func (r *postgresRepository) Create(ctx context.Context, u *User) error {
	// TODO: insert into users — o trigger trg_users_seat_limit no banco
	// já barra estourar o seat_limit; ainda assim, checar antes evita um
	// round-trip de erro pro caminho feliz.
	return nil
}

func (r *postgresRepository) SetActive(ctx context.Context, id string, active bool) error {
	// TODO: update users set is_active = $2 where id = $1
	return nil
}
