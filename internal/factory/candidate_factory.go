package factory

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/candidate"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/llmusage"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// InitCandidateFactory recebe o *pgxpool.Pool concreto (não só database.DB) porque Decide e
// RegisterPublicApplication precisam rodar suas escritas (mover de fase/reprovar + gravar
// candidate_decisions, ou criar talent+candidate juntos numa candidatura pública) numa transação
// real (database.WithTx) — mesmo padrão de InitCampaignFactory.
func InitCandidateFactory(pool *pgxpool.Pool, extractor llm.Extractor, assessor llm.Assessor) (*candidate.Handler, candidate.Service) {
	repo := candidate.NewRepository(pool)
	withTx := func(ctx context.Context, fn func(db database.DB) error) error {
		return database.WithTx(ctx, pool, fn)
	}
	// Repositório de uso construído sobre o pool, não sobre a tx: o gasto com o provedor tem que
	// ficar registrado mesmo quando a transação da candidatura faz rollback (ver migrations/0008).
	service := candidate.NewService(repo, withTx, extractor, assessor, llmusage.NewRepository(pool))
	return candidate.NewHandler(service), service
}
