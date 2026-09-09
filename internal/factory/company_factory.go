package factory

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/company"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/user"
)

// InitCompanyFactory recebe o repository de user pronto porque cadastrar uma
// empresa cria o primeiro RH (owner) junto — ver company.Service.Register.
// Recebe o *pgxpool.Pool concreto (não só database.DB) porque Register
// precisa rodar as duas escritas numa transação real (database.WithTx).
func InitCompanyFactory(pool *pgxpool.Pool, users user.Repository) *company.Handler {
	repo := company.NewRepository(pool)
	withTx := func(ctx context.Context, fn func(db database.DB) error) error {
		return database.WithTx(ctx, pool, fn)
	}
	service := company.NewService(repo, users, withTx)
	return company.NewHandler(service)
}
