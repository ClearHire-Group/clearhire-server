package factory

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/domain/company"
)

func InitCompanyFactory(db *pgxpool.Pool) *company.Handler {
	repo := company.NewRepository(db)
	service := company.NewService(repo)
	return company.NewHandler(service)
}
