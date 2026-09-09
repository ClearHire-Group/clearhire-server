package middleware

import "github.com/gofiber/fiber/v2"

// Tenant é uma segunda trava, depois de Auth: exige que companyID já esteja
// no contexto. Redundante enquanto as rotas estão montadas corretamente, mas
// barato — e é o tipo de checagem que vale existir mesmo redundante, porque
// o custo de um erro de escopo entre empresas é alto (ver README).
func Tenant() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if CompanyID(c) == "" {
			return fiber.ErrUnauthorized
		}
		return c.Next()
	}
}
