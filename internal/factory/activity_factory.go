package factory

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/domain/activity"
)

func InitActivityFactory(db *pgxpool.Pool) *activity.Handler {
	repo := activity.NewRepository(db)
	service := activity.NewService(repo)
	return activity.NewHandler(service)
}
