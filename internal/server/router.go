package server

import (
	"github.com/gofiber/fiber/v2"

	"github.com/ClearHire-Group/clearhire-server/internal/config"
	"github.com/ClearHire-Group/clearhire-server/internal/factory"
	"github.com/ClearHire-Group/clearhire-server/internal/middleware"
)

// RegisterRoutes pluga cada domínio no grupo /api/v1. Login e cadastro de
// empresa ficam públicos, porque são o que cria a sessão; o resto passa por
// Auth + Tenant.
func RegisterRoutes(app *fiber.App, f *factory.Factory, cfg *config.Config) {
	v1 := app.Group("/api/v1")

	f.AuthHandler.RegisterRoutes(v1)
	f.CompanyHandler.RegisterPublicRoutes(v1)
	// Candidatura pública por link de campanha — candidato anônimo, sem JWT nenhum.
	f.CampaignHandler.RegisterPublicRoutes(v1)
	f.CandidateHandler.RegisterPublicRoutes(v1)

	protected := v1.Group("", middleware.Auth(cfg.JWTSecret), middleware.Tenant())
	f.CompanyHandler.RegisterProfileRoutes(protected)
	f.UserHandler.RegisterRoutes(protected)
	f.CampaignHandler.RegisterRoutes(protected)
	f.CampaignHandler.RegisterReportsRoutes(protected)
	f.CandidateHandler.RegisterRoutes(protected)
	f.CandidateHandler.RegisterCampaignRoutes(protected)
	f.TalentHandler.RegisterRoutes(protected)
	f.DashboardHandler.RegisterRoutes(protected)
	f.ActivityHandler.RegisterRoutes(protected)
}
