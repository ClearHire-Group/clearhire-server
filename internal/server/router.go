package server

import (
	"github.com/gofiber/fiber/v2"

	"github.com/ClearHire-Group/clearhire-server/internal/factory"
	"github.com/ClearHire-Group/clearhire-server/internal/middleware"
)

// RegisterRoutes pluga cada domínio no grupo /api/v1. Rotas de auth ficam
// públicas; o resto passa por Auth + Tenant.
func RegisterRoutes(app *fiber.App, f *factory.Factory) {
	v1 := app.Group("/api/v1")

	f.AuthHandler.RegisterRoutes(v1)

	protected := v1.Group("", middleware.Auth(), middleware.Tenant())
	f.CompanyHandler.RegisterRoutes(protected)
	f.UserHandler.RegisterRoutes(protected)
	f.CampaignHandler.RegisterRoutes(protected)
	f.CandidateHandler.RegisterRoutes(protected)
	f.TalentHandler.RegisterRoutes(protected)
}
