package database

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WithTx roda fn dentro de uma única transação Postgres, só dando commit se
// fn devolver nil. Um repository construído com o DB passado pra fn
// participa da mesma transação — é a "porta aberta" que o comentário em
// DB já previa: pgx.Tx satisfaz a mesma interface que *pgxpool.Pool.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx DB) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op se já deu commit

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
