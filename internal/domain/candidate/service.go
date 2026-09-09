package candidate

import "context"

type Service interface {
	Get(ctx context.Context, companyID, id string) (*Candidate, error)
	ListByCampaign(ctx context.Context, companyID, campaignID, phaseKey string) ([]Candidate, error)
	Decide(ctx context.Context, companyID, id string, req DecideRequest, decidedByUserID string) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Get(ctx context.Context, companyID, id string) (*Candidate, error) {
	return s.repo.FindByID(ctx, companyID, id)
}

func (s *service) ListByCampaign(ctx context.Context, companyID, campaignID, phaseKey string) ([]Candidate, error) {
	return s.repo.ListByCampaign(ctx, companyID, campaignID, phaseKey)
}

func (s *service) Decide(ctx context.Context, companyID, id string, req DecideRequest, decidedByUserID string) error {
	return s.repo.RecordDecision(ctx, companyID, id, req, decidedByUserID)
}
