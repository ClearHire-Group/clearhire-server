package company

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/ClearHire-Group/clearhire-server/internal/middleware"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/response"
	"github.com/ClearHire-Group/clearhire-server/pkg/validator"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

// RegisterPublicRoutes pluga o cadastro de empresa — a única rota deste
// domínio que roda ANTES de existir sessão (é ela que cria a primeira).
func (h *Handler) RegisterPublicRoutes(router fiber.Router) {
	router.Group("/companies", middleware.RateLimit(5, time.Minute)).Post("/", h.Register)
}

// RegisterRoutes pluga o restante, protegido por autenticação.
func (h *Handler) RegisterRoutes(router fiber.Router) {
	group := router.Group("/companies")
	group.Get("/:id", h.Get)
	group.Patch("/:id/culture", h.UpdateCultureProfile)
}

// Register é a única rota deste domínio fora do grupo autenticado — é o
// cadastro que cria a empresa e o primeiro RH.
func (h *Handler) Register(c *fiber.Ctx) error {
	var req RegisterCompanyRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "dados obrigatórios faltando")
	}

	result, err := h.service.Register(c.Context(), req)
	if err != nil {
		var appErr *apperror.AppError
		if errors.As(err, &appErr) {
			return response.Err(c, appErr.Code, appErr.Message)
		}
		return response.Err(c, fiber.StatusInternalServerError, "erro interno")
	}
	return response.Created(c, toResponse(result))
}

func (h *Handler) Get(c *fiber.Ctx) error {
	result, err := h.service.Get(c.Context(), c.Params("id"))
	if err != nil {
		var appErr *apperror.AppError
		if errors.As(err, &appErr) {
			return response.Err(c, appErr.Code, appErr.Message)
		}
		return response.Err(c, fiber.StatusInternalServerError, "erro interno")
	}
	return response.OK(c, toResponse(result))
}

func (h *Handler) UpdateCultureProfile(c *fiber.Ctx) error {
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}
