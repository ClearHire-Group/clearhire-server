package factory

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/candidate"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// InitCandidateFactory recebe o *pgxpool.Pool concreto (não só database.DB) porque Decide e
// RegisterPublicApplication precisam rodar suas escritas (mover de fase/reprovar + gravar
// candidate_decisions, ou criar talent+candidate juntos numa candidatura pública) numa transação
// real (database.WithTx) — mesmo padrão de InitCampaignFactory.
func InitCandidateFactory(pool *pgxpool.Pool, extractor llm.Extractor) *candidate.Handler {
	repo := candidate.NewRepository(pool)
	withTx := func(ctx context.Context, fn func(db database.DB) error) error {
		return database.WithTx(ctx, pool, fn)
	}
	service := candidate.NewService(repo, withTx, extractor)
	return candidate.NewHandler(service)
}
