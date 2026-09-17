package activity

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/ClearHire-Group/clearhire-server/internal/middleware"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/response"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(router fiber.Router) {
	router.Group("/dashboard").Get("/activity", h.List)
}

func (h *Handler) List(c *fiber.Ctx) error {
	items, err := h.service.List(c.Context(), middleware.CompanyID(c), c.QueryInt("limit"))
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toResponse(items))
}

func (h *Handler) respondError(c *fiber.Ctx, err error) error {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		return response.Err(c, appErr.Code, appErr.Message)
	}
	return response.Err(c, fiber.StatusInternalServerError, "erro interno")
}
