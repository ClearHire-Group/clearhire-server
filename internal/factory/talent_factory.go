package factory

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/domain/talent"
)

func InitTalentFactory(db *pgxpool.Pool) *talent.Handler {
	repo := talent.NewRepository(db)
	service := talent.NewService(repo)
	return talent.NewHandler(service)
}
