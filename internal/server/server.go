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
	// Origem explícita + credenciais habilitadas — necessário pro cookie
	// httpOnly de refresh token funcionar em qualquer topologia que não
	// seja "mesma origem via proxy" (dev usa o proxy do ng serve e nem
	// depende disto, mas uma implantação futura pode ser cross-origin de
	// verdade). Nota: o Fiber recusa subir (panic) se AllowCredentials for
	// true com AllowOrigins resolvendo pra "*" — rede de segurança own: um
	// CORS_ORIGIN mal configurado vira crash no boot, não buraco silencioso.
	app.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.CORSOrigin,
		AllowCredentials: true,
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE",
	}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	return app
}
