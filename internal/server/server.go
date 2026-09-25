// Package server monta a instância do Fiber e o middleware global. A
// composição de rotas de domínio fica em router.go, separado da criação do
// app em si.
package server

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
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
		// Sem timeouts o Fiber espera para sempre: uma conexão que manda 1 byte por vez (slowloris)
		// ocuparia um worker indefinidamente. O ReadTimeout cobre o upload de currículo (até 5MB);
		// o WriteTimeout tem que ficar ACIMA do teto total de uma chamada de IA (llm.Limits, 40s) —
		// senão o servidor cortaria a resposta de uma candidatura que a IA ainda está processando.
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
		// c.IP() alimenta o rate limit por IP. Atrás de um proxy, sem isto todos os clientes
		// aparecem com o IP do proxy (um balde só para o mundo); confiando em qualquer header, o
		// cliente escolhe o próprio IP. Então: só se lê o header de IP quando a conexão vem de um
		// proxy da lista, e o valor precisa ser um IP válido (senão o Fiber devolve o string bruto,
		// e cada valor inventado viraria um balde novo).
		ProxyHeader:             cfg.ClientIPHeader,
		EnableTrustedProxyCheck: true,
		TrustedProxies:          cfg.TrustedProxies,
		EnableIPValidation:      true,
	})

	app.Use(recover.New())
	// Compressão das respostas: JSON de listagem é muito repetitivo e cai para ~5-10% do tamanho (a
	// listagem de 3 mil talentos passava de 8 MB crus). LevelBestSpeed porque o gargalo é a rede, não a
	// taxa de compressão.
	//
	// /auth fica de fora de propósito (BREACH): comprimir uma resposta que carrega um segredo (o token
	// de acesso no corpo do login) junto com dado controlado por quem pede deixa o tamanho comprimido
	// vazar o segredo. Nenhuma outra rota devolve segredo no corpo.
	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
		Next: func(c *fiber.Ctx) bool {
			return strings.HasPrefix(c.Path(), "/api/v1/auth")
		},
	}))
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
