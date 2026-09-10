package factory

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/campaign"
)

// InitCampaignFactory recebe o *pgxpool.Pool concreto (não só database.DB) porque Create precisa
// rodar a inserção de campanha + fases numa transação real (database.WithTx) — mesmo padrão de
// InitCompanyFactory.
func InitCampaignFactory(pool *pgxpool.Pool) *campaign.Handler {
	repo := campaign.NewRepository(pool)
	withTx := func(ctx context.Context, fn func(db database.DB) error) error {
		return database.WithTx(ctx, pool, fn)
	}
	service := campaign.NewService(repo, withTx)
	return campaign.NewHandler(service)
}
