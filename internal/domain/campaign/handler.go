package campaign

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
	group := router.Group("/campaigns")
	group.Get("/", h.List)
	group.Post("/", h.Create)
	group.Get("/:id", h.Get)
	group.Post("/:id/toggle-pause", h.TogglePause)
}

func (h *Handler) List(c *fiber.Ctx) error {
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}

func (h *Handler) Create(c *fiber.Ctx) error {
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}

func (h *Handler) Get(c *fiber.Ctx) error {
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}

func (h *Handler) TogglePause(c *fiber.Ctx) error {
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}
