package auth

import "time"

// LoginRequest é o payload de POST /auth/login.
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// TokenPair é o resultado interno de Login/Refresh — nunca serializado
// direto (tags json:"-" de propósito). O handler é quem decide o que vai
// pra onde: AccessToken no corpo JSON, RefreshToken num cookie httpOnly.
// Serializar isto direto (response.OK(c, result)) voltaria a vazar o
// refresh token no body — é exatamente o que este tipo existe pra evitar.
type TokenPair struct {
	AccessToken      string    `json:"-"`
	RefreshToken     string    `json:"-"`
	RefreshExpiresAt time.Time `json:"-"`
}

// AcceptInvitationRequest é o payload de POST /auth/invitations/:token/accept
// — o segundo RH define a própria senha e entra como member.
type AcceptInvitationRequest struct {
	Name     string `json:"name" validate:"required"`
	Password string `json:"password" validate:"required,min=8"`
}

// AcceptInvitationResponse devolve o e-mail do convite aceito — o front usa isso pra pré-preencher
// a tela de login (aceitar convite não loga automaticamente, mesmo padrão de POST /companies).
type AcceptInvitationResponse struct {
	Email string `json:"email"`
}

// ForgotPasswordRequest é o payload de POST /auth/password-reset.
type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email"`
}

// ForgotPasswordResponse nunca revela se o e-mail existe — ResetLink só vem preenchido em
// development (sem SMTP real, ver pkg/mailer.LogSender).
type ForgotPasswordResponse struct {
	ResetLink string `json:"resetLink,omitempty"`
}

// ResetPasswordRequest é o payload de POST /auth/password-reset/:token — o token vem da URL,
// mesmo padrão de AcceptInvitationRequest.
type ResetPasswordRequest struct {
	NewPassword string `json:"newPassword" validate:"required,min=8"`
}
