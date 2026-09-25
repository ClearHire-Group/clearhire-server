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

// GlobalRateLimit conta TODAS as requisições da rota juntas, ignorando a origem.
//
// RateLimit acima é por IP, e por isso não protege contra a forma de abuso que importa numa rota
// pública que dispara chamada paga: 100 origens distintas passam 100x pelo limite por IP sem
// encostar nele. Este teto é o que limita o estrago total por unidade de tempo, independentemente
// de quantas origens participem.
//
// É um limite grosseiro de contenção, não de justiça: ele deve ficar bem acima do tráfego legítimo
// esperado, porque quando dispara também recusa candidato de verdade. O controle fino de custo é o
// teto de orçamento por empresa (ver migrations/0009); este aqui é o disjuntor.
func GlobalRateLimit(max int, expiration time.Duration) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: expiration,
		KeyGenerator: func(*fiber.Ctx) string {
			return "global"
		},
		LimitReached: func(c *fiber.Ctx) error {
			return response.Err(c, fiber.StatusTooManyRequests, "sistema temporariamente sobrecarregado, tente novamente em instantes")
		},
	})
}

// UserRateLimit conta por USUÁRIO autenticado, não por IP. Serve às rotas protegidas que disparam
// trabalho pago (a análise por IA): vários recrutadores atrás do mesmo IP de escritório não podem
// dividir um balde, e um recrutador não escapa do limite trocando de rede. Tem que rodar depois de
// Auth; sem usuário no contexto, cai no IP em vez de compartilhar uma chave vazia.
func UserRateLimit(max int, expiration time.Duration) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: expiration,
		KeyGenerator: func(c *fiber.Ctx) string {
			if id := UserID(c); id != "" {
				return "user:" + id
			}
			return "ip:" + c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return response.Err(c, fiber.StatusTooManyRequests, "muitas solicitações, aguarde um instante e tente novamente")
		},
	})
}
