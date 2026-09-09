// Package server monta a instância do Fiber e o middleware global. A
// composição de rotas de domínio fica em router.go, separado da criação do
// app em si.
package server

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/ClearHire-Group/clearhire-server/internal/config"
)

func New(cfg *config.Config) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName: "clearhire-server",
	})

	app.Use(recover.New())
	app.Use(logger.New())
	app.Use(cors.New())

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	return app
}
