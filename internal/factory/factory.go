// Package factory é o composition root da aplicação: o único lugar que
// conhece a cadeia completa repository → service → handler de cada domínio.
// Nenhum outro pacote monta essas dependências à mão — cmd/api/main.go só
// chama factory.New(db, cfg) e passa o resultado pro router.
package factory

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/config"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/auth"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/campaign"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/candidate"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/company"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/talent"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/user"
)

// Factory expõe os handlers já montados de cada domínio — é só o que o
// router precisa pra registrar as rotas.
type Factory struct {
	AuthHandler      *auth.Handler
	CompanyHandler   *company.Handler
	UserHandler      *user.Handler
	CampaignHandler  *campaign.Handler
	CandidateHandler *candidate.Handler
	TalentHandler    *talent.Handler
}

func New(db *pgxpool.Pool, cfg *config.Config) *Factory {
	// user.Repository é compartilhado entre auth (login busca por e-mail) e
	// company (cadastro cria o primeiro RH) — uma instância só, sem duplicar
	// a conexão nem a lógica de acesso a `users`.
	userRepo := newUserRepository(db)

	return &Factory{
		AuthHandler:      InitAuthFactory(db, userRepo, cfg.JWTSecret, cfg.Env != "development"),
		CompanyHandler:   InitCompanyFactory(db, userRepo),
		UserHandler:      InitUserFactory(userRepo),
		CampaignHandler:  InitCampaignFactory(db),
		CandidateHandler: InitCandidateFactory(db),
		TalentHandler:    InitTalentFactory(db),
	}
}
