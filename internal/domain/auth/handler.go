package auth

import (
	"github.com/gofiber/fiber/v2"

	"github.com/ClearHire-Group/clearhire-server/pkg/response"
)

// Handler expõe os endpoints HTTP de autenticação. Sem lógica de negócio
// aqui — só parse de request, chamada ao Service e formatação de resposta.
type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes pluga os endpoints de auth no grupo de rotas recebido.
func (h *Handler) RegisterRoutes(router fiber.Router) {
	group := router.Group("/auth")
	group.Post("/login", h.Login)
	group.Post("/invitations/:token/accept", h.AcceptInvitation)
}

func (h *Handler) Login(c *fiber.Ctx) error {
	var req LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	// TODO: validar req, chamar h.service.Login, devolver os tokens
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}

func (h *Handler) AcceptInvitation(c *fiber.Ctx) error {
	// TODO: parse do token da URL + payload, chamar h.service.AcceptInvitation
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}
