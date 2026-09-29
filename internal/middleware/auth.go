// Package middleware reúne os interceptadores de request compartilhados por
// toda a API — autenticação, escopo de tenant, log e recuperação de panic.
package middleware

import (
	"context"
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

// ActiveChecker responde se o assento do usuário continua ativo. Recebe o id que veio do JWT já
// validado, nunca um valor cru do cliente.
type ActiveChecker func(ctx context.Context, userID string) (bool, error)

// Auth valida o JWT da requisição e injeta o usuário autenticado no contexto.
// Todo handler/service protegido lê o tenant daqui (via CompanyID(c)) — nunca
// de um parâmetro que o cliente possa forjar (ver README, seção Multi-tenancy).
//
// Confere o assento a CADA requisição, e não só na emissão do token: desativar um assento (ou
// trocar a senha de uma conta invadida) revoga os refresh tokens, mas o access token que já está
// na mão de alguém continua valendo até 15min. Sem esta checagem, "desativar" não corta o acesso
// na hora — quem foi desligado segue lendo candidato e banco de talentos nesse intervalo.
//
// Custa uma consulta por requisição autenticada. É consulta de uma coluna por chave primária, e
// toda rota protegida já vai ao banco de qualquer forma.
func Auth(jwtSecret string, isActive ActiveChecker) fiber.Handler {
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

		// Falha FECHADO, ao contrário da leitura de orçamento (que falha aberto de propósito):
		// aqui não há o que perder recusando durante uma instabilidade do banco, porque toda rota
		// protegida precisaria do banco logo em seguida de qualquer jeito.
		active, err := isActive(c.Context(), claims.UserID)
		if err != nil {
			return response.Err(c, fiber.StatusServiceUnavailable, "não foi possível validar a sessão agora")
		}
		if !active {
			return response.Err(c, fiber.StatusUnauthorized, "esta conta não está mais ativa")
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
