// Package middleware reúne os interceptadores de request compartilhados por
// toda a API — autenticação, escopo de tenant, log e recuperação de panic.
package middleware

import "github.com/gofiber/fiber/v2"

// Auth valida o JWT da requisição e injeta o usuário autenticado no contexto.
// TODO: decodificar o token, validar assinatura/expiração e popular c.Locals.
func Auth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}
