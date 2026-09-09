package factory

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/domain/auth"
)

// InitAuthFactory monta a cadeia repository → service → handler do domínio
// de autenticação e devolve só o handler, que é o que o router precisa.
func InitAuthFactory(db *pgxpool.Pool) *auth.Handler {
	repo := auth.NewRepository(db)
	service := auth.NewService(repo)
	return auth.NewHandler(service)
}
