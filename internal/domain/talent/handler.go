package talent

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

// RegisterCampaignMatchRoutes pluga o match reverso sob o prefixo /campaigns — path fixo que o
// frontend já chama (clearhire-app core/http-api.service.ts, getReverseMatchForNewCampaign). Fica
// no domínio talent (dono dos dados e da fórmula de score) apesar do prefixo de campaign, mesmo
// padrão de composição já usado no projeto (ver campaign.Handler.RegisterReportsRoutes, que também
// mora num domínio e é plugado à parte no router).
func (h *Handler) RegisterCampaignMatchRoutes(router fiber.Router) {
	router.Post("/campaigns/reverse-match", h.ReverseMatch)
	// Etapa 2 (leitura de IA sob demanda): por usuário, não por IP — mesmo padrão de
	// RegisterManual, que também dispara chamada paga. 10/min é folgado pro uso real (no máximo 5
	// talentos por chamada, ver maxAssessedTalentsPerRequest) e apertado o bastante pra não virar
	// um jeito barato de martelar o orçamento mensal da empresa.
	router.Post("/campaigns/:id/talent-recommendations/assess",
		middleware.UserRateLimit(10, time.Minute), h.AssessTalentRecommendations)
}

func (h *Handler) ReverseMatch(c *fiber.Ctx) error {
	var req ReverseMatchRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "dados obrigatórios faltando ou inválidos")
	}

	matches, err := h.service.ReverseMatch(c.Context(), middleware.CompanyID(c),
		MatchCriteria{Title: req.Title, Modality: req.Modality, Seniority: req.Seniority, Requirements: req.Requirements})
	if err != nil {
		return h.respondError(c, err)
	}
	now := time.Now()
	out := make([]*TalentMatchResponse, len(matches))
	for i := range matches {
		out[i] = toMatchResponse(&matches[i], now)
	}
	return response.OK(c, out)
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

func (h *Handler) AssessTalentRecommendations(c *fiber.Ctx) error {
	campaignID, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}
	var req AssessTalentsRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "dados obrigatórios faltando ou inválidos")
	}

	recs, err := h.service.AssessForCampaign(c.Context(), middleware.CompanyID(c), campaignID, req.TalentIDs)
	if err != nil {
		return h.respondError(c, err)
	}
	now := time.Now()
	out := make([]*TalentRecommendationResponse, len(recs))
	for i := range recs {
		out[i] = toRecommendationResponse(&recs[i], now)
	}
	return response.OK(c, out)
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
