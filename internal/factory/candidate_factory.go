package factory

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/domain/candidate"
)

func InitCandidateFactory(db *pgxpool.Pool) *candidate.Handler {
	repo := candidate.NewRepository(db)
	service := candidate.NewService(repo)
	return candidate.NewHandler(service)
}
