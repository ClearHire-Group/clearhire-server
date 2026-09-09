// Package database cuida da conexão com o PostgreSQL. Nenhuma query de
// domínio mora aqui — isso é responsabilidade de cada repository.
package database

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool abre o pool de conexões usado por todos os repositories.
// Chamado uma única vez, na inicialização (ver internal/factory).
func NewPostgresPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	return pool, nil
}
