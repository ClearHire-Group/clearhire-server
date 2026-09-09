package user

// InviteUserRequest é o payload de POST /companies/:id/invitations — o
// owner convida o segundo assento de RH.
type InviteUserRequest struct {
	Email string `json:"email" validate:"required,email"`
}

// UpdateProfileRequest é o payload de PATCH /users/:id — tela de perfil.
type UpdateProfileRequest struct {
	Name string `json:"name" validate:"required"`
}
