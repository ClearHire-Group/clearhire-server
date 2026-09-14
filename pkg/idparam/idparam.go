// Package idparam valida parâmetros de rota que devem ser UUID antes de qualquer query chegar ao
// banco. Sem isso, um :id malformado (ex.: "not-a-uuid") vira erro do driver Postgres contra uma
// coluna uuid, que sobe como 500 genérico — o cliente mandou um dado inválido, não é uma falha do
// servidor (achado num pentest de rotina).
package idparam

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/ClearHire-Group/clearhire-server/pkg/response"
)

// Valid lê o parâmetro de rota `name` e confirma que é um UUID bem formado. Quando não é, já
// escreve a resposta 400 no `c` e devolve ok=false — o handler só precisa checar ok antes de
// seguir, sem duplicar a resposta de erro em cada chamador.
func Valid(c *fiber.Ctx, name string) (id string, ok bool) {
	id = c.Params(name)
	if _, err := uuid.Parse(id); err != nil {
		response.Err(c, fiber.StatusBadRequest, "identificador inválido")
		return "", false
	}
	return id, true
}
