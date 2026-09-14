package user

// InviteUserRequest é o payload de POST /users/invitations — o owner
// convida o segundo assento de RH pelo e-mail.
type InviteUserRequest struct {
	Email string `json:"email" validate:"required,email,max=254"`
}

// UpdateMeRequest é o payload de PATCH /users/me — cada RH só edita o
// próprio nome, nunca o de outro (sem :id de rota, id vem do token).
type UpdateMeRequest struct {
	Name string `json:"name" validate:"required,max=200"`
}

// DeactivateRequest é o payload de DELETE /users/:id — desativar um assento é destrutivo o
// bastante (a pessoa perde acesso na hora) pra exigir que o owner reprove a própria senha, não só
// o JWT já em mãos. Password é a senha do CALLER (owner autenticado), nunca do alvo. max=72 pelo
// mesmo motivo de auth.LoginRequest — é o limite real que o bcrypt usa.
type DeactivateRequest struct {
	Password string `json:"password" validate:"required,max=72"`
}

// MeResponse é o que GET/PATCH /users/me devolvem.
type MeResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  Role   `json:"role"`
}

func toMeResponse(u *User) *MeResponse {
	return &MeResponse{ID: u.ID, Name: u.Name, Email: u.Email, Role: u.Role}
}

// TeamMemberResponse é uma linha de GET /users — RH ativo/inativo e convite
// pendente projetados na mesma forma (ver TeamMember no model.go).
type TeamMemberResponse struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Email  string           `json:"email"`
	Role   Role             `json:"role"`
	Status TeamMemberStatus `json:"status"`
}

func toTeamMemberResponse(m *TeamMember) *TeamMemberResponse {
	return &TeamMemberResponse{ID: m.ID, Name: m.Name, Email: m.Email, Role: m.Role, Status: m.Status}
}

// InviteResponse é o que POST /users/invitations devolve. InviteLink só vem
// preenchido em development — fora disso o convite é sempre enviado por
// e-mail (ver mailer.Sender), nunca exposto na resposta HTTP.
type InviteResponse struct {
	InviteLink string `json:"inviteLink,omitempty"`
}
