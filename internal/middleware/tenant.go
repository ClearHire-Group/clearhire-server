package middleware

import "github.com/gofiber/fiber/v2"

// Tenant garante que toda query de domínio saia escopada pela empresa do
// usuário autenticado (ver db/schema.sql — isolamento entre empresas).
// TODO: ler company_id do usuário autenticado (via Auth) e injetar no contexto
// pra todos os repositories usarem no WHERE.
func Tenant() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}
