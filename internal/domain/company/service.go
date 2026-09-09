package company

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/user"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
)

// uniqueViolationCode é o SQLSTATE do Postgres pra unique_violation.
const uniqueViolationCode = "23505"

type Service interface {
	Get(ctx context.Context, id string) (*Company, error)
	Register(ctx context.Context, req RegisterCompanyRequest) (*Company, error)
	UpdateCultureProfile(ctx context.Context, id string, req UpdateCultureProfileRequest) error
}

type service struct {
	repo  Repository
	users user.Repository
	// withTx roda as duas escritas de Register (empresa + primeiro RH) numa
	// única transação — repository construído com o `db` recebido pela
	// closure participa da mesma transação, sem o pacote company depender
	// de pgxpool diretamente (ver internal/database.WithTx).
	withTx func(ctx context.Context, fn func(db database.DB) error) error
}

// NewService recebe o repository de user além do próprio: cadastrar uma
// empresa sempre cria o primeiro RH (role=owner) junto — são duas tabelas,
// uma operação de negócio.
func NewService(repo Repository, users user.Repository, withTx func(ctx context.Context, fn func(db database.DB) error) error) Service {
	return &service{repo: repo, users: users, withTx: withTx}
}

func (s *service) Get(ctx context.Context, id string) (*Company, error) {
	c, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, apperror.NotFound("empresa não encontrada")
	}
	return c, nil
}

func (s *service) Register(ctx context.Context, req RegisterCompanyRequest) (*Company, error) {
	// Pre-check best-effort — a garantia de verdade é a constraint unique em
	// users.email (citext, global), aplicada dentro da transação abaixo.
	// Isto aqui só existe pra falhar rápido e barato no caso comum.
	if existing, _ := s.users.FindByEmail(ctx, req.OwnerEmail); existing != nil {
		return nil, apperror.BadRequest("já existe uma conta com este e-mail")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.OwnerPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, apperror.Internal("falha ao processar senha")
	}

	c := &Company{Name: req.CompanyName}
	owner := &user.User{
		Name:         req.OwnerName,
		Email:        req.OwnerEmail,
		PasswordHash: string(hash),
		Role:         user.RoleOwner,
	}

	// Empresa + primeiro RH numa única transação: se o insert do usuário
	// falhar depois do da empresa (ex: corrida na unicidade do e-mail), o
	// rollback desfaz os dois — nunca sobra empresa órfã sem dono.
	err = s.withTx(ctx, func(db database.DB) error {
		txCompanies := NewRepository(db)
		txUsers := user.NewRepository(db)

		if err := txCompanies.Create(ctx, c); err != nil {
			return err
		}

		owner.CompanyID = c.ID
		if err := txUsers.Create(ctx, owner); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
				return apperror.BadRequest("já existe uma conta com este e-mail")
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (s *service) UpdateCultureProfile(ctx context.Context, id string, req UpdateCultureProfileRequest) error {
	return s.repo.UpdateCultureProfile(ctx, id, req)
}
