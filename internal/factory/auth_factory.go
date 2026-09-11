package factory

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/auth"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/user"
	"github.com/ClearHire-Group/clearhire-server/pkg/mailer"
)

// InitAuthFactory recebe o repository de user pronto: login precisa buscar
// o usuário por e-mail, e essa leitura pertence ao domínio user, não a auth.
// cookieSecure liga a flag Secure do cookie de refresh token (exigida fora
// de development, onde a API real corre atrás de HTTPS). Recebe o
// *pgxpool.Pool concreto (não só database.DB) porque AcceptInvitation
// precisa rodar criação de usuário + aceite de convite numa transação real
// (database.WithTx), mesmo padrão de InitCompanyFactory.
func InitAuthFactory(pool *pgxpool.Pool, users user.Repository, jwtSecret string, cookieSecure, isDevelopment bool, frontendBaseURL string, sender mailer.Sender) *auth.Handler {
	repo := auth.NewRepository(pool)
	withTx := func(ctx context.Context, fn func(db database.DB) error) error {
		return database.WithTx(ctx, pool, fn)
	}
	service := auth.NewService(repo, users, jwtSecret, withTx, sender, frontendBaseURL)
	return auth.NewHandler(service, cookieSecure, isDevelopment)
}
