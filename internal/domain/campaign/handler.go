package campaign

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/ClearHire-Group/clearhire-server/internal/middleware"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/idparam"
	"github.com/ClearHire-Group/clearhire-server/pkg/response"
	"github.com/ClearHire-Group/clearhire-server/pkg/validator"
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
	group.Patch("/:id", h.Update)
	group.Patch("/:id/phases", h.UpdatePhases)
	group.Post("/:id/toggle-pause", h.TogglePause)
	group.Post("/:id/public-application-link", h.SetPublicLink)
}

// RegisterPublicRoutes pluga a única leitura sem tenant deste domínio — GET /public/campaigns/:id,
// chamada pelo candidato anônimo antes de se candidatar. Mesmo padrão de company.Handler.RegisterPublicRoutes.
func (h *Handler) RegisterPublicRoutes(router fiber.Router) {
	router.Group("/public/campaigns").Get("/:id", h.GetPublicInfo)
}

// RegisterReportsRoutes pluga os agregados que a tela Relatórios consome — moram aqui porque
// reaproveitam exatamente a mesma agregação por fase que List já calcula, sem estado próprio que
// justifique um pacote `reports` separado.
func (h *Handler) RegisterReportsRoutes(router fiber.Router) {
	group := router.Group("/reports")
	group.Get("/funnel-summary", h.FunnelSummary)
	group.Get("/campaign-performance", h.CampaignPerformance)
}

func (h *Handler) List(c *fiber.Ctx) error {
	views, err := h.service.List(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return h.respondError(c, err)
	}
	result := make([]*Response, len(views))
	for i := range views {
		result[i] = toResponse(&views[i])
	}
	return response.OK(c, result)
}

func (h *Handler) Create(c *fiber.Ctx) error {
	var req CreateCampaignRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "dados obrigatórios faltando ou inválidos")
	}

	view, err := h.service.Create(c.Context(), middleware.CompanyID(c), middleware.UserID(c), req)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.Created(c, toResponse(view))
}

func (h *Handler) Get(c *fiber.Ctx) error {
	id, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}
	view, err := h.service.Get(c.Context(), middleware.CompanyID(c), id)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toResponse(view))
}

func (h *Handler) Update(c *fiber.Ctx) error {
	id, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}
	var req UpdateCampaignRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "dados obrigatórios faltando ou inválidos")
	}

	view, err := h.service.Update(c.Context(), middleware.CompanyID(c), id, req)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toResponse(view))
}

func (h *Handler) UpdatePhases(c *fiber.Ctx) error {
	id, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}
	var req UpdatePhasesRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "dados obrigatórios faltando ou inválidos")
	}

	view, err := h.service.UpdatePhases(c.Context(), middleware.CompanyID(c), id, req.PhaseKeys)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toResponse(view))
}

func (h *Handler) TogglePause(c *fiber.Ctx) error {
	id, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}
	view, err := h.service.TogglePause(c.Context(), middleware.CompanyID(c), id, middleware.UserID(c))
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toResponse(view))
}

func (h *Handler) SetPublicLink(c *fiber.Ctx) error {
	id, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}
	var req SetPublicLinkRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}

	view, err := h.service.SetPublicApplicationsEnabled(c.Context(), middleware.CompanyID(c), id, req.Enabled)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toResponse(view))
}

func (h *Handler) GetPublicInfo(c *fiber.Ctx) error {
	id, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}
	info, err := h.service.GetPublicInfo(c.Context(), id)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toPublicInfoResponse(info))
}

// parseReportPeriod lê `from`/`to` da query string em RFC3339 (ex.: 2026-09-01T00:00:00-03:00).
// São instantes COM fuso, não datas soltas: quem sabe em que fuso o usuário está é o navegador,
// então o recorte chega pronto e nenhuma query precisa chutar um timezone. Ausentes = sem limite.
func parseReportPeriod(c *fiber.Ctx) (ReportPeriod, error) {
	var period ReportPeriod
	for _, field := range []struct {
		name   string
		target **time.Time
	}{{"from", &period.From}, {"to", &period.To}} {
		raw := c.Query(field.name)
		if raw == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return period, apperror.BadRequest("parâmetro '" + field.name + "' inválido: use data no formato RFC3339")
		}
		*field.target = &parsed
	}
	if period.From != nil && period.To != nil && !period.To.After(*period.From) {
		return period, apperror.BadRequest("o fim do período precisa ser posterior ao início")
	}
	return period, nil
}

func (h *Handler) FunnelSummary(c *fiber.Ctx) error {
	period, err := parseReportPeriod(c)
	if err != nil {
		return h.respondError(c, err)
	}
	summary, err := h.service.FunnelSummary(c.Context(), middleware.CompanyID(c), period)
	if err != nil {
		return h.respondError(c, err)
	}
	result := make([]FunnelSummaryResponse, len(summary))
	for i, pc := range summary {
		result[i] = FunnelSummaryResponse{Key: pc.Key, Num: pc.Position, Label: phaseLabels[pc.Key], Count: pc.Count}
	}
	return response.OK(c, result)
}

func (h *Handler) CampaignPerformance(c *fiber.Ctx) error {
	period, err := parseReportPeriod(c)
	if err != nil {
		return h.respondError(c, err)
	}
	rows, err := h.service.CampaignPerformance(c.Context(), middleware.CompanyID(c), period)
	if err != nil {
		return h.respondError(c, err)
	}
	result := make([]*CampaignPerformanceResponse, len(rows))
	for i := range rows {
		result[i] = toPerformanceResponse(&rows[i])
	}
	return response.OK(c, result)
}

func (h *Handler) respondError(c *fiber.Ctx, err error) error {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		return response.Err(c, appErr.Code, appErr.Message)
	}
	return response.Err(c, fiber.StatusInternalServerError, "erro interno")
}
