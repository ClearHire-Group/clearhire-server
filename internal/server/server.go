// Package server monta a instância do Fiber e o middleware global. A
// composição de rotas de domínio fica em router.go, separado da criação do
// app em si.
package server

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/ClearHire-Group/clearhire-server/internal/config"
)

func New(cfg *config.Config) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName: "clearhire-server",
		// Corta o corpo da requisição antes mesmo de tentar parsear/validar — barata contra payload
		// gigante de propósito. Fiber não tem um jeito limpo de sobrescrever isto por rota (o
		// limite é aplicado no nível do fasthttp, antes do roteamento), então o teto é global:
		// 6MB dá folga confortável pro upload de currículo em PDF (handler ainda valida um teto
		// próprio de 5MB, ver candidate/handler.go) sem deixar passar coisa absurdamente grande —
		// bem abaixo do limite de 32MB da API da Claude.
		BodyLimit: 6 * 1024 * 1024,
	})

	app.Use(recover.New())
	app.Use(logger.New())
	// Headers de resposta padrão (X-Content-Type-Options, X-Frame-Options, HSTS quando servido
	// por HTTPS, etc.) — API pura em JSON não tem muita superfície de HTML/frame pra proteger,
	// mas são de graça e fecham um gap real (achado num pentest: nenhum header de segurança
	// estava presente antes disso).
	app.Use(helmet.New())
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
