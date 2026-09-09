// Package middleware reúne os interceptadores de request compartilhados por
// toda a API — autenticação, escopo de tenant, log e recuperação de panic.
package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/ClearHire-Group/clearhire-server/pkg/response"
	"github.com/ClearHire-Group/clearhire-server/pkg/token"
)

const (
	localUserID    = "userID"
	localCompanyID = "companyID"
	localRole      = "role"
)

// Auth valida o JWT da requisição e injeta o usuário autenticado no contexto.
// Todo handler/service protegido lê o tenant daqui (via CompanyID(c)) — nunca
// de um parâmetro que o cliente possa forjar (ver README, seção Multi-tenancy).
func Auth(jwtSecret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		raw, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || raw == "" {
			return response.Err(c, fiber.StatusUnauthorized, "token ausente")
		}

		claims, err := token.Parse(jwtSecret, raw)
		if err != nil {
			return response.Err(c, fiber.StatusUnauthorized, "token inválido ou expirado")
		}

		c.Locals(localUserID, claims.UserID)
		c.Locals(localCompanyID, claims.CompanyID)
		c.Locals(localRole, claims.Role)
		return c.Next()
	}
}

// UserID devolve o id do usuário autenticado, populado por Auth.
func UserID(c *fiber.Ctx) string {
	v, _ := c.Locals(localUserID).(string)
	return v
}

// CompanyID devolve o tenant do usuário autenticado — a base de todo escopo
// de dado. Handlers e services usam este valor, nunca um vindo do request.
func CompanyID(c *fiber.Ctx) string {
	v, _ := c.Locals(localCompanyID).(string)
	return v
}

// Role devolve o papel do usuário autenticado (owner/member).
func Role(c *fiber.Ctx) string {
	v, _ := c.Locals(localRole).(string)
	return v
}
