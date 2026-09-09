package factory

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/domain/campaign"
)

func InitCampaignFactory(db *pgxpool.Pool) *campaign.Handler {
	repo := campaign.NewRepository(db)
	service := campaign.NewService(repo)
	return campaign.NewHandler(service)
}
