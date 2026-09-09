package factory

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/domain/user"
)

func InitUserFactory(db *pgxpool.Pool) *user.Handler {
	repo := user.NewRepository(db)
	service := user.NewService(repo)
	return user.NewHandler(service)
}
