package auth

import "context"

// Service concentra a lógica de autenticação — hash/verificação de senha,
// emissão e validação de token, aceite de convite. O handler não conhece
// nenhum desses detalhes, só chama o Service.
type Service interface {
	Login(ctx context.Context, req LoginRequest) (*LoginResponse, error)
	AcceptInvitation(ctx context.Context, token string, req AcceptInvitationRequest) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	// TODO: buscar usuário por e-mail, comparar hash de senha, emitir JWT
	return nil, nil
}

func (s *service) AcceptInvitation(ctx context.Context, token string, req AcceptInvitationRequest) error {
	// TODO: validar token de user_invitations, criar o segundo usuário
	return nil
}
