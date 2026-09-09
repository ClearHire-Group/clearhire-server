package candidate

import (
	"github.com/gofiber/fiber/v2"

	"github.com/ClearHire-Group/clearhire-server/pkg/response"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(router fiber.Router) {
	group := router.Group("/candidates")
	group.Get("/:id", h.Get)
	group.Post("/:id/decisions", h.Decide)
}

func (h *Handler) Get(c *fiber.Ctx) error {
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}

func (h *Handler) Decide(c *fiber.Ctx) error {
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}
