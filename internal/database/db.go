package database

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DB é o subconjunto de *pgxpool.Pool que um repository precisa. Tanto o pool
// quanto uma pgx.Tx satisfazem essa interface — um repository construído com
// uma Tx em vez do pool participa de uma transação maior sem saber disso.
// Hoje nenhum fluxo usa transação entre domínios; esta interface deixa o
// caminho aberto pra quando precisar, sem repository nenhum mudar de forma.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
