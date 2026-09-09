package auth

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository isola o acesso a refresh_tokens/password_reset_tokens — o
// service nunca fala SQL diretamente.
type Repository interface {
	StoreRefreshToken(ctx context.Context, userID, tokenHash string) error
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
}

type postgresRepository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) StoreRefreshToken(ctx context.Context, userID, tokenHash string) error {
	// TODO: insert into refresh_tokens
	return nil
}

func (r *postgresRepository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	// TODO: update refresh_tokens set revoked_at = now()
	return nil
}
