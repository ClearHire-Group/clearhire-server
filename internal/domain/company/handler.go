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
// O teto por IP contém uma origem insistente; o global contém a enumeração DISTRIBUÍDA, que é o
// abuso que importa aqui: a resposta "já existe uma conta com este e-mail" é um oráculo de
// existência de conta, e sem o balde global N origens passam N vezes pelo limite por IP sem
// encostar nele. Cadastro de empresa é evento raro, então 30/min para a plataforma inteira está
// muito acima do uso legítimo.
func (h *Handler) RegisterPublicRoutes(router fiber.Router) {
	router.Group("/companies",
		middleware.GlobalRateLimit(30, time.Minute),
		middleware.RateLimit(5, time.Minute),
	).Post("/", h.Register)
}

// RegisterProfileRoutes pluga o perfil cultural da empresa — sempre a empresa do
// usuário autenticado (middleware.CompanyID), nunca um :id de rota. Não existe "buscar
// outra empresa por id" neste app: cada RH só enxerga a própria.
func (h *Handler) RegisterProfileRoutes(router fiber.Router) {
	group := router.Group("/company-profile")
	group.Get("/", h.GetProfile)
	group.Post("/", h.UpdateProfile)
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
		return h.respondError(c, err)
	}
	return response.Created(c, toResponse(result))
}

func (h *Handler) GetProfile(c *fiber.Ctx) error {
	result, err := h.service.GetProfile(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toProfileResponse(result))
}

func (h *Handler) UpdateProfile(c *fiber.Ctx) error {
	var req UpdateCultureProfileRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "dados inválidos")
	}

	result, err := h.service.UpdateProfile(c.Context(), middleware.CompanyID(c), req)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toProfileResponse(result))
}

func (h *Handler) respondError(c *fiber.Ctx, err error) error {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		return response.Err(c, appErr.Code, appErr.Message)
	}
	return response.Err(c, fiber.StatusInternalServerError, "erro interno")
}
