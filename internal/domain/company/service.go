package company

import "context"

type Service interface {
	Get(ctx context.Context, id string) (*Company, error)
	Register(ctx context.Context, req RegisterCompanyRequest) (*Company, error)
	UpdateCultureProfile(ctx context.Context, id string, req UpdateCultureProfileRequest) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Get(ctx context.Context, id string) (*Company, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *service) Register(ctx context.Context, req RegisterCompanyRequest) (*Company, error) {
	// TODO: criar company + primeiro usuário (role=owner) numa transação
	return nil, nil
}

func (s *service) UpdateCultureProfile(ctx context.Context, id string, req UpdateCultureProfileRequest) error {
	return s.repo.UpdateCultureProfile(ctx, id, req)
}
