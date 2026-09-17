package activity

import (
	"context"

	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
)

const (
	DefaultLimit = 50
	MaxLimit     = 200
)

type Service interface {
	List(ctx context.Context, companyID string, limit int) ([]Item, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

// List trunca o limite em vez de rejeitar valor fora da faixa: `?limit=` é conveniência de leitura
// de um feed append-only, não entrada de domínio — devolver 400 pra quem pediu 9999 só transforma
// um detalhe de paginação em erro de tela.
func (s *service) List(ctx context.Context, companyID string, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	items, err := s.repo.ListByCompany(ctx, companyID, limit)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar atividade")
	}
	return items, nil
}
