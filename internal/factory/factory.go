// Package factory é o composition root da aplicação: o único lugar que
// conhece a cadeia completa repository → service → handler de cada domínio.
// Nenhum outro pacote monta essas dependências à mão — cmd/api/main.go só
// chama factory.New(db) e passa o resultado pro router.
package factory

import (
	"github.com/jackc/pgx/v5/pgxpool"

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
	AuthHandler     *auth.Handler
	CompanyHandler  *company.Handler
	UserHandler     *user.Handler
	CampaignHandler *campaign.Handler
	CandidateHandler *candidate.Handler
	TalentHandler   *talent.Handler
}

func New(db *pgxpool.Pool) *Factory {
	return &Factory{
		AuthHandler:      InitAuthFactory(db),
		CompanyHandler:   InitCompanyFactory(db),
		UserHandler:      InitUserFactory(db),
		CampaignHandler:  InitCampaignFactory(db),
		CandidateHandler: InitCandidateFactory(db),
		TalentHandler:    InitTalentFactory(db),
	}
}
