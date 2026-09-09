package talent

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
	group := router.Group("/talents")
	group.Get("/", h.List)
	group.Post("/", h.RegisterManual)
	group.Get("/search", h.Search)
	group.Get("/:id", h.Get)
}

func (h *Handler) List(c *fiber.Ctx) error {
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}

func (h *Handler) RegisterManual(c *fiber.Ctx) error {
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}

func (h *Handler) Search(c *fiber.Ctx) error {
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}

func (h *Handler) Get(c *fiber.Ctx) error {
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}
