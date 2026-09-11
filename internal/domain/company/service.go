package company

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/user"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
)

// uniqueViolationCode é o SQLSTATE do Postgres pra unique_violation.
const uniqueViolationCode = "23505"

// maxCultureValues é um limite de bom senso pros chips de "valores organizacionais" —
// não especificado em lugar nenhum, só evita abuso (uma lista de 200 chips não serve
// pra nada na UI).
const maxCultureValues = 10

type Service interface {
	Register(ctx context.Context, req RegisterCompanyRequest) (*Company, error)
	// GetProfile e UpdateProfile são o que a tela Configurações (perfil cultural) usa —
	// sempre escopado pela empresa autenticada, nunca por um :id de rota.
	GetProfile(ctx context.Context, companyID string) (*Profile, error)
	UpdateProfile(ctx context.Context, companyID string, req UpdateCultureProfileRequest) (*Profile, error)
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

func (s *service) GetProfile(ctx context.Context, companyID string) (*Profile, error) {
	c, err := s.repo.FindByID(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar perfil da empresa")
	}
	if c == nil {
		return nil, apperror.NotFound("empresa não encontrada")
	}

	values, err := s.repo.ValuesByCompany(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar valores organizacionais")
	}

	return &Profile{
		ID:             c.ID,
		Name:           c.Name,
		Tone:           c.CultureTone,
		ImportanceNote: c.CultureImportanceNote,
		Values:         values,
	}, nil
}

func (s *service) UpdateProfile(ctx context.Context, companyID string, req UpdateCultureProfileRequest) (*Profile, error) {
	values := normalizeCultureValues(req.Values)

	// Uma escrita em companies + substituir company_culture_values inteira: mesma
	// justificativa de atomicidade do Register — uma falha no meio não pode deixar
	// tone/importanceNote gravados com metade dos valores antigos e metade dos novos.
	err := s.withTx(ctx, func(db database.DB) error {
		return NewRepository(db).UpdateCultureProfile(ctx, companyID, strings.TrimSpace(req.Tone), strings.TrimSpace(req.ImportanceNote), values)
	})
	if err != nil {
		return nil, apperror.Internal("falha ao salvar perfil da empresa")
	}

	return s.GetProfile(ctx, companyID)
}

// normalizeCultureValues remove espaços/entradas vazias e deduplica mantendo a ordem
// original (a ordem é o que vira `position` na hora de persistir). Validação de
// tamanho/quantidade já aconteceu via tag do validator no handler — isto aqui só limpa
// o que passou pela validação estrutural mas ainda pode ter espaço sobrando ou repetição.
func normalizeCultureValues(raw []string) []string {
	seen := make(map[string]bool, len(raw))
	values := make([]string, 0, len(raw))
	for _, v := range raw {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		values = append(values, trimmed)
		if len(values) == maxCultureValues {
			break
		}
	}
	return values
}
