package database

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DB é o subconjunto de *pgxpool.Pool que um repository precisa. Tanto o pool
// quanto uma pgx.Tx satisfazem essa interface — um repository construído com
// uma Tx em vez do pool participa de uma transação maior sem saber disso.
// É o que permite transação ENTRE domínios: candidate/campaign constroem um
// activity.Repository com a própria tx, então a linha do feed de atividade e a
// ação que ela registra fazem commit (ou rollback) juntas.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
