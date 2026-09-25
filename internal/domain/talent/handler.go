package talent

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/ClearHire-Group/clearhire-server/internal/middleware"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/idparam"
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
	// Cadastro manual pode disparar extração paga: limitado por usuário, como a análise por IA.
	group.Post("/", middleware.UserRateLimit(20, time.Minute), h.RegisterManual)
	// Rotas fixas ANTES de /:id, senão "search"/"coverage" seriam lidos como id (e dariam 400).
	group.Get("/search", h.NotImplemented)
	group.Get("/coverage", h.Coverage)
	group.Get("/:id", h.Get)
	group.Get("/:id/similar", h.NotImplemented)
	group.Post("/:id/first-contact", h.MarkFirstContact)
}

func (h *Handler) List(c *fiber.Ctx) error {
	talents, err := h.service.List(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return h.respondError(c, err)
	}
	now := time.Now()
	out := make([]*TalentResponse, len(talents))
	for i := range talents {
		out[i] = toResponse(&talents[i], now)
	}
	return response.OK(c, out)
}

func (h *Handler) Get(c *fiber.Ctx) error {
	id, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}
	t, err := h.service.Get(c.Context(), middleware.CompanyID(c), id)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toResponse(t, time.Now()))
}

func (h *Handler) RegisterManual(c *fiber.Ctx) error {
	var req RegisterManualRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	t, err := h.service.RegisterManual(c.Context(), middleware.CompanyID(c),
		ManualInput{Name: req.Name, RawProfileText: req.RawProfileText, ContextNote: req.ContextNote})
	if err != nil {
		return h.respondError(c, err)
	}
	return response.Created(c, toResponse(t, time.Now()))
}

func (h *Handler) MarkFirstContact(c *fiber.Ctx) error {
	id, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}
	t, err := h.service.MarkFirstContact(c.Context(), middleware.CompanyID(c), id)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toResponse(t, time.Now()))
}

func (h *Handler) Coverage(c *fiber.Ctx) error {
	entries, err := h.service.Coverage(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return h.respondError(c, err)
	}
	out := make([]coverageResponse, len(entries))
	for i, e := range entries {
		out[i] = coverageResponse{SkillTerm: e.SkillTerm, Count: e.Count}
	}
	return response.OK(c, out)
}

// NotImplemented: busca em linguagem natural e "parecidos com" ainda não existem no backend. 501 é o
// código que o front trata como "sem resultados" (HttpApiService.emptyOnUnavailable).
func (h *Handler) NotImplemented(c *fiber.Ctx) error {
	return response.Err(c, fiber.StatusNotImplemented, "não implementado")
}

func (h *Handler) respondError(c *fiber.Ctx, err error) error {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		if len(appErr.Fields) > 0 {
			return response.ErrFields(c, appErr.Code, appErr.Message, appErr.Fields)
		}
		if appErr.Field != "" {
			return response.ErrFields(c, appErr.Code, appErr.Message, map[string]string{appErr.Field: appErr.Message})
		}
		return response.Err(c, appErr.Code, appErr.Message)
	}
	return response.Err(c, fiber.StatusInternalServerError, "erro interno")
}
