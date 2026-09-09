package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
)

// RefreshToken é uma linha de refresh_tokens. RevokedAt é ponteiro porque a
// coluna é nulável — um token nunca revogado tem RevokedAt == nil.
type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// Repository isola o acesso a refresh_tokens — o service nunca fala SQL
// diretamente. password_reset_tokens fica pra quando o fluxo "esqueci minha
// senha" for implementado; não faz parte do login.
type Repository interface {
	StoreRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	FindRefreshTokenByHash(ctx context.Context, tokenHash string) (*RefreshToken, error)
	// RevokeAllForUser mata toda a família de refresh tokens de um usuário —
	// usado quando um token já revogado é reapresentado (reuso = sinal de
	// token roubado/duplicado), não só o token único usado numa troca normal.
	RevokeAllForUser(ctx context.Context, userID string) error
}

type postgresRepository struct {
	db database.DB
}

func NewRepository(db database.DB) Repository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) StoreRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		insert into refresh_tokens (user_id, token_hash, expires_at)
		values ($1, $2, $3)
	`, userID, tokenHash, expiresAt)
	return err
}

func (r *postgresRepository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := r.db.Exec(ctx, "update refresh_tokens set revoked_at = now() where token_hash = $1", tokenHash)
	return err
}

func (r *postgresRepository) FindRefreshTokenByHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	row := r.db.QueryRow(ctx, `
		select id, user_id, token_hash, expires_at, revoked_at
		from refresh_tokens where token_hash = $1
	`, tokenHash)
	var rt RefreshToken
	err := row.Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.ExpiresAt, &rt.RevokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &rt, nil
}

func (r *postgresRepository) RevokeAllForUser(ctx context.Context, userID string) error {
	_, err := r.db.Exec(ctx, "update refresh_tokens set revoked_at = now() where user_id = $1 and revoked_at is null", userID)
	return err
}
