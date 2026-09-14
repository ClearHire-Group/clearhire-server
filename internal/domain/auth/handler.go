package auth

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/ClearHire-Group/clearhire-server/internal/middleware"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/response"
	"github.com/ClearHire-Group/clearhire-server/pkg/validator"
)

const (
	refreshCookieName = "refreshToken"
	// Escopo estreito de propósito: só os próprios endpoints de auth
	// recebem este cookie. Nenhuma outra rota da API (campanhas, talentos,
	// etc.) nunca vê o refresh token, mesmo que algo nelas seja comprometido.
	refreshCookiePath = "/api/v1/auth"
)

// Handler expõe os endpoints HTTP de autenticação. Sem lógica de negócio
// aqui — só parse de request, chamada ao Service e formatação de resposta.
type Handler struct {
	service Service
	// cookieSecure liga a flag Secure do cookie de refresh — exigida fora de
	// development, onde a API real corre atrás de HTTPS.
	cookieSecure bool
	// exposeResetLinks controla se POST /auth/password-reset devolve o link cru na resposta —
	// só em development, onde não existe SMTP real (ver pkg/mailer.LogSender).
	exposeResetLinks bool
}

func NewHandler(service Service, cookieSecure, exposeResetLinks bool) *Handler {
	return &Handler{service: service, cookieSecure: cookieSecure, exposeResetLinks: exposeResetLinks}
}

// RegisterRoutes pluga os endpoints de auth no grupo de rotas recebido.
// Estas rotas ficam FORA do grupo autenticado — é aqui que a sessão nasce,
// se renova (refresh) e termina (logout). Refresh/logout precisam ficar
// públicos porque o objetivo deles é funcionar exatamente quando o access
// token já não é mais válido.
func (h *Handler) RegisterRoutes(router fiber.Router) {
	group := router.Group("/auth")
	group.Post("/login", middleware.RateLimit(10, time.Minute), h.Login)
	group.Post("/refresh", h.Refresh)
	group.Post("/logout", h.Logout)
	// O token em si (32 bytes aleatórios, só o hash fica no banco) não é adivinhável por força
	// bruta em nenhum ritmo de rate limit — o limite aqui é defesa em profundidade contra abuso/
	// spam de requisições nessas rotas públicas, mesmo padrão de login/password-reset abaixo.
	group.Post("/invitations/:token/accept", middleware.RateLimit(10, time.Minute), h.AcceptInvitation)
	group.Post("/password-reset", middleware.RateLimit(5, time.Minute), h.RequestPasswordReset)
	group.Post("/password-reset/:token", middleware.RateLimit(10, time.Minute), h.ConfirmPasswordReset)
}

func (h *Handler) Login(c *fiber.Ctx) error {
	var req LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "e-mail e senha são obrigatórios")
	}

	result, err := h.service.Login(c.Context(), req)
	if err != nil {
		return h.respondError(c, err)
	}
	h.setRefreshCookie(c, result.RefreshToken, result.RefreshExpiresAt)
	return response.OK(c, fiber.Map{"accessToken": result.AccessToken})
}

func (h *Handler) Refresh(c *fiber.Ctx) error {
	raw := c.Cookies(refreshCookieName)
	result, err := h.service.Refresh(c.Context(), raw)
	if err != nil {
		h.clearRefreshCookie(c) // token ausente/inválido/reusado — nunca deixa um cookie morto no browser
		return h.respondError(c, err)
	}
	h.setRefreshCookie(c, result.RefreshToken, result.RefreshExpiresAt)
	return response.OK(c, fiber.Map{"accessToken": result.AccessToken})
}

func (h *Handler) Logout(c *fiber.Ctx) error {
	raw := c.Cookies(refreshCookieName)
	_ = h.service.Logout(c.Context(), raw) // melhor esforço — logout sempre "funciona" do ponto de vista do cliente
	h.clearRefreshCookie(c)
	return response.OK(c, nil)
}

// setRefreshCookie e clearRefreshCookie nunca usam c.ClearCookie(): ele não
// define Path, e por RFC 6265 um Set-Cookie só sobrescreve/apaga um cookie
// existente se Path bater exatamente — ClearCookie() jamais apagaria este
// cookie (escopado a refreshCookiePath). Também nunca usar MaxAge negativo
// pra apagar: o Fiber só escreve Max-Age no header quando MaxAge > 0: um
// valor negativo é silenciosamente ignorado. Only Expires no passado funciona.
func (h *Handler) setRefreshCookie(c *fiber.Ctx, value string, expires time.Time) {
	c.Cookie(&fiber.Cookie{
		Name:     refreshCookieName,
		Value:    value,
		Path:     refreshCookiePath,
		Expires:  expires,
		Secure:   h.cookieSecure,
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
	})
}

func (h *Handler) clearRefreshCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		Expires:  time.Now().Add(-time.Hour),
		Secure:   h.cookieSecure,
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
	})
}

func (h *Handler) respondError(c *fiber.Ctx, err error) error {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		return response.Err(c, appErr.Code, appErr.Message)
	}
	return response.Err(c, fiber.StatusInternalServerError, "erro interno")
}

func (h *Handler) AcceptInvitation(c *fiber.Ctx) error {
	var req AcceptInvitationRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "nome e senha (mínimo 8 caracteres) são obrigatórios")
	}

	email, err := h.service.AcceptInvitation(c.Context(), c.Params("token"), req)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, &AcceptInvitationResponse{Email: email})
}

// RequestPasswordReset sempre responde 200 — nunca revela se o e-mail existe (mesma filosofia de
// Login). resetLink só vem preenchido em development (ver Handler.exposeResetLinks).
func (h *Handler) RequestPasswordReset(c *fiber.Ctx) error {
	var req ForgotPasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "e-mail inválido")
	}

	link, err := h.service.RequestPasswordReset(c.Context(), req.Email)
	if err != nil {
		return h.respondError(c, err)
	}

	resp := &ForgotPasswordResponse{}
	if h.exposeResetLinks {
		resp.ResetLink = link
	}
	return response.OK(c, resp)
}

func (h *Handler) ConfirmPasswordReset(c *fiber.Ctx) error {
	var req ResetPasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "senha deve ter no mínimo 8 caracteres")
	}

	if err := h.service.ConfirmPasswordReset(c.Context(), c.Params("token"), req.NewPassword); err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, nil)
}
