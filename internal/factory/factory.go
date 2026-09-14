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
	"github.com/ClearHire-Group/clearhire-server/internal/domain/dashboard"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/talent"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/user"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm/deterministic"
	"github.com/ClearHire-Group/clearhire-server/pkg/mailer"
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
	DashboardHandler *dashboard.Handler
}

func New(db *pgxpool.Pool, cfg *config.Config) *Factory {
	// user.Repository é compartilhado entre auth (login busca por e-mail) e
	// company (cadastro cria o primeiro RH) — uma instância só, sem duplicar
	// a conexão nem a lógica de acesso a `users`.
	userRepo := newUserRepository(db)
	isDevelopment := cfg.Env == "development"
	// Sender único, compartilhado por todo mundo que "manda e-mail" (convite, redefinição de
	// senha) — hoje só loga (sem SMTP configurado ainda, ver pkg/mailer.LogSender).
	sender := mailer.LogSender{}
	// CORSOrigin já é a origem do front (http://localhost:4200 em dev) — reaproveitada como base
	// dos links de convite/redefinição de senha, sem precisar de uma env var nova.
	frontendBaseURL := cfg.CORSOrigin
	// Extractor único, compartilhado só pelo domínio candidate (é o único que lê currículo).
	// deterministic.New() é a implementação em uso agora: regex + dicionário, zero custo de
	// chamada de API (decisão de produto — a extração via IA cobra por token, à parte de qualquer
	// assinatura de uso do Claude Code, e o MVP não depende disso). pkg/llm/anthropicadapter
	// continua existindo, intocado, em "stand by" — trocar de volta é só trocar esta linha por
	// anthropicadapter.New(cfg.AnthropicAPIKey, cfg.AnthropicModel), já que os dois implementam a
	// mesma interface llm.Extractor.
	extractor := deterministic.New()

	return &Factory{
		AuthHandler:      InitAuthFactory(db, userRepo, cfg.JWTSecret, !isDevelopment, isDevelopment, frontendBaseURL, sender),
		CompanyHandler:   InitCompanyFactory(db, userRepo),
		UserHandler:      InitUserFactory(userRepo, sender, frontendBaseURL, isDevelopment),
		CampaignHandler:  InitCampaignFactory(db),
		CandidateHandler: InitCandidateFactory(db, extractor),
		TalentHandler:    InitTalentFactory(db),
		DashboardHandler: InitDashboardFactory(db),
	}
}
