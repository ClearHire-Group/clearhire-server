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
