package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"

	"github.com/ClearHire-Group/clearhire-server/pkg/response"
)

// RateLimit devolve um middleware que aceita no máximo `max` requisições por
// IP a cada `expiration`, respondendo no envelope padrão da API em vez do
// 429 cru default do Fiber. Usado nas rotas públicas de auth/cadastro — as
// únicas que um atacante pode martelar sem precisar de sessão nenhuma.
func RateLimit(max int, expiration time.Duration) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: expiration,
		LimitReached: func(c *fiber.Ctx) error {
			return response.Err(c, fiber.StatusTooManyRequests, "muitas tentativas, aguarde um instante e tente novamente")
		},
	})
}
