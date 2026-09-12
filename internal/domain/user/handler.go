package user

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
	// exposeInviteLinks controla se POST /users/invitations devolve o link cru na resposta —
	// só em development, onde não existe SMTP real (ver pkg/mailer.LogSender). Mesmo padrão de
	// auth.Handler.cookieSecure.
	exposeInviteLinks bool
}

func NewHandler(service Service, exposeInviteLinks bool) *Handler {
	return &Handler{service: service, exposeInviteLinks: exposeInviteLinks}
}

func (h *Handler) RegisterRoutes(router fiber.Router) {
	group := router.Group("/users")
	group.Get("/", h.ListTeam)
	group.Get("/me", h.Me)
	group.Patch("/me", h.UpdateMe)
	group.Post("/invitations", h.Invite)
	// Rota de convite específica ANTES de "/:id": tem um segmento a mais ("/invitations/:id"), não
	// colide de verdade com "/:id" (que só casa um segmento), mas mantém as duas rotas de convite
	// juntas facilita ler.
	group.Delete("/invitations/:id", h.CancelInvitation)
	// Rate limit aqui de propósito: a partir desta mudança o endpoint aceita uma senha no corpo
	// (ver DeactivateRequest) — sem limite, seria um oráculo de força bruta contra a senha do
	// próprio owner escondido atrás de uma rota autenticada. Mesmo padrão de POST /auth/login.
	group.Delete("/:id", middleware.RateLimit(5, time.Minute), h.Deactivate)
}

func (h *Handler) Me(c *fiber.Ctx) error {
	u, err := h.service.Me(c.Context(), middleware.CompanyID(c), middleware.UserID(c))
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toMeResponse(u))
}

func (h *Handler) UpdateMe(c *fiber.Ctx) error {
	var req UpdateMeRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "nome é obrigatório")
	}

	companyID, userID := middleware.CompanyID(c), middleware.UserID(c)
	if err := h.service.UpdateMe(c.Context(), companyID, userID, req); err != nil {
		return h.respondError(c, err)
	}
	u, err := h.service.Me(c.Context(), companyID, userID)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toMeResponse(u))
}

func (h *Handler) ListTeam(c *fiber.Ctx) error {
	members, err := h.service.ListTeam(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return h.respondError(c, err)
	}
	result := make([]*TeamMemberResponse, len(members))
	for i := range members {
		result[i] = toTeamMemberResponse(&members[i])
	}
	return response.OK(c, result)
}

func (h *Handler) Invite(c *fiber.Ctx) error {
	if middleware.Role(c) != string(RoleOwner) {
		return response.Err(c, fiber.StatusForbidden, "apenas o owner pode convidar novos RHs")
	}

	var req InviteUserRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "e-mail inválido")
	}

	link, err := h.service.Invite(c.Context(), middleware.CompanyID(c), middleware.UserID(c), req)
	if err != nil {
		return h.respondError(c, err)
	}

	resp := &InviteResponse{}
	if h.exposeInviteLinks {
		resp.InviteLink = link
	}
	return response.Created(c, resp)
}

func (h *Handler) CancelInvitation(c *fiber.Ctx) error {
	if middleware.Role(c) != string(RoleOwner) {
		return response.Err(c, fiber.StatusForbidden, "apenas o owner pode cancelar convites")
	}

	if err := h.service.CancelInvitation(c.Context(), middleware.CompanyID(c), c.Params("id")); err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, nil)
}

func (h *Handler) Deactivate(c *fiber.Ctx) error {
	if middleware.Role(c) != string(RoleOwner) {
		return response.Err(c, fiber.StatusForbidden, "apenas o owner pode desativar assentos")
	}

	var req DeactivateRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "senha é obrigatória")
	}

	err := h.service.Deactivate(c.Context(), middleware.CompanyID(c), c.Params("id"), middleware.UserID(c), req.Password)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, nil)
}

func (h *Handler) respondError(c *fiber.Ctx, err error) error {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		return response.Err(c, appErr.Code, appErr.Message)
	}
	return response.Err(c, fiber.StatusInternalServerError, "erro interno")
}
