package talent

import (
	"context"

	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
)

// RegisterManualFunc grava um cadastro manual e devolve o id do talento. Implementado pelo domínio
// candidate (candidate.Service.RegisterManualTalent), que é onde mora toda escrita de perfil —
// ligado na factory, sem import entre os dois domínios.
type RegisterManualFunc func(ctx context.Context, companyID string, in ManualInput) (string, error)

type Service interface {
	List(ctx context.Context, companyID string) ([]Talent, error)
	Get(ctx context.Context, companyID, id string) (*Talent, error)
	RegisterManual(ctx context.Context, companyID string, in ManualInput) (*Talent, error)
	MarkFirstContact(ctx context.Context, companyID, id string) (*Talent, error)
	Coverage(ctx context.Context, companyID string) ([]CoverageEntry, error)
}

type service struct {
	repo           Repository
	registerManual RegisterManualFunc
}

func NewService(repo Repository, registerManual RegisterManualFunc) Service {
	return &service{repo: repo, registerManual: registerManual}
}

func (s *service) List(ctx context.Context, companyID string) ([]Talent, error) {
	talents, err := s.repo.ListInBank(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao listar talentos")
	}
	return talents, nil
}

func (s *service) Get(ctx context.Context, companyID, id string) (*Talent, error) {
	t, err := s.repo.FindInBank(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar talento")
	}
	if t == nil {
		return nil, apperror.NotFound("talento não encontrado")
	}
	return t, nil
}

func (s *service) RegisterManual(ctx context.Context, companyID string, in ManualInput) (*Talent, error) {
	id, err := s.registerManual(ctx, companyID, in)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, companyID, id)
}

func (s *service) MarkFirstContact(ctx context.Context, companyID, id string) (*Talent, error) {
	found, err := s.repo.MarkFirstContact(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao registrar contato")
	}
	if !found {
		return nil, apperror.NotFound("talento não encontrado")
	}
	return s.Get(ctx, companyID, id)
}

func (s *service) Coverage(ctx context.Context, companyID string) ([]CoverageEntry, error) {
	entries, err := s.repo.Coverage(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular cobertura")
	}
	return entries, nil
}
