package factory

import (
	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/auth"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/user"
)

// InitAuthFactory recebe o repository de user pronto: login precisa buscar
// o usuário por e-mail, e essa leitura pertence ao domínio user, não a auth.
// cookieSecure liga a flag Secure do cookie de refresh token (exigida fora
// de development, onde a API real corre atrás de HTTPS).
func InitAuthFactory(db database.DB, users user.Repository, jwtSecret string, cookieSecure bool) *auth.Handler {
	repo := auth.NewRepository(db)
	service := auth.NewService(repo, users, jwtSecret)
	return auth.NewHandler(service, cookieSecure)
}
