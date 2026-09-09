package user

import "context"

type Service interface {
	Get(ctx context.Context, id string) (*User, error)
	Invite(ctx context.Context, companyID string, req InviteUserRequest) error
	UpdateProfile(ctx context.Context, id string, req UpdateProfileRequest) error
	Deactivate(ctx context.Context, id string) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Get(ctx context.Context, id string) (*User, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *service) Invite(ctx context.Context, companyID string, req InviteUserRequest) error {
	// TODO: conferir seat_limit vs. assentos ativos, criar user_invitation
	return nil
}

func (s *service) UpdateProfile(ctx context.Context, id string, req UpdateProfileRequest) error {
	// TODO: atualizar nome do usuário
	return nil
}

func (s *service) Deactivate(ctx context.Context, id string) error {
	return s.repo.SetActive(ctx, id, false)
}
