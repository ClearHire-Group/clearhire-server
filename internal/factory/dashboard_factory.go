package factory

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/domain/dashboard"
)

func InitDashboardFactory(db *pgxpool.Pool) *dashboard.Handler {
	repo := dashboard.NewRepository(db)
	service := dashboard.NewService(repo)
	return dashboard.NewHandler(service)
}
