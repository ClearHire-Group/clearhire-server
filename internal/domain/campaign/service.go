package campaign

import "context"

type Service interface {
	Get(ctx context.Context, companyID, id string) (*Campaign, error)
	List(ctx context.Context, companyID string) ([]Campaign, error)
	Create(ctx context.Context, companyID, createdByUserID string, req CreateCampaignRequest) (*Campaign, error)
	TogglePause(ctx context.Context, companyID, id string) (Status, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Get(ctx context.Context, companyID, id string) (*Campaign, error) {
	return s.repo.FindByID(ctx, companyID, id)
}

func (s *service) List(ctx context.Context, companyID string) ([]Campaign, error) {
	return s.repo.ListByCompany(ctx, companyID)
}

func (s *service) Create(ctx context.Context, companyID, createdByUserID string, req CreateCampaignRequest) (*Campaign, error) {
	// TODO: montar Campaign + Phase a partir do request e persistir
	return nil, nil
}

func (s *service) TogglePause(ctx context.Context, companyID, id string) (Status, error) {
	// TODO: buscar status atual, alternar ativa/pausada, nunca tocar encerrada
	return "", nil
}
