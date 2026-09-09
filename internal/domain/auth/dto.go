package auth

// LoginRequest é o payload de POST /auth/login.
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// LoginResponse é o payload de resposta do login — os tokens de sessão.
type LoginResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// AcceptInvitationRequest é o payload de POST /auth/invitations/:token/accept
// — o segundo RH define a própria senha e entra como member.
type AcceptInvitationRequest struct {
	Name     string `json:"name" validate:"required"`
	Password string `json:"password" validate:"required,min=8"`
}
