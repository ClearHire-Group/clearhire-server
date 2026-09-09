package talent

import "context"

type Service interface {
	Get(ctx context.Context, companyID, id string) (*Talent, error)
	List(ctx context.Context, companyID string) ([]Talent, error)
	RegisterManual(ctx context.Context, companyID string, req RegisterManualRequest) (*Talent, error)
	Search(ctx context.Context, companyID string, query string) ([]Talent, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Get(ctx context.Context, companyID, id string) (*Talent, error) {
	return s.repo.FindByID(ctx, companyID, id)
}

func (s *service) List(ctx context.Context, companyID string) ([]Talent, error) {
	return s.repo.ListByCompany(ctx, companyID)
}

func (s *service) RegisterManual(ctx context.Context, companyID string, req RegisterManualRequest) (*Talent, error) {
	// TODO: extrair campos estruturados de RawProfileText (ingestão via LLM),
	// montar Talent e persistir
	return nil, nil
}

func (s *service) Search(ctx context.Context, companyID string, query string) ([]Talent, error) {
	return s.repo.Search(ctx, companyID, query)
}
