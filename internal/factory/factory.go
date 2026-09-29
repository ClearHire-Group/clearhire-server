// Package factory é o composition root da aplicação: o único lugar que
// conhece a cadeia completa repository → service → handler de cada domínio.
// Nenhum outro pacote monta essas dependências à mão — cmd/api/main.go só
// chama factory.New(db, cfg) e passa o resultado pro router.
package factory

import (
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/config"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/activity"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/auth"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/campaign"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/candidate"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/company"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/dashboard"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/talent"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/user"
	"github.com/ClearHire-Group/clearhire-server/internal/middleware"
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
	ActivityHandler  *activity.Handler
	// IsUserActive alimenta o middleware de autenticação, que confere o assento a cada requisição.
	// Sai daqui (e não de um import direto de user no server) porque o composition root é quem
	// monta dependência — router só liga o que já veio pronto.
	IsUserActive middleware.ActiveChecker
}

func New(db *pgxpool.Pool, cfg *config.Config) *Factory {
	// user.Repository é compartilhado entre auth (login busca por e-mail) e
	// company (cadastro cria o primeiro RH) — uma instância só, sem duplicar
	// a conexão nem a lógica de acesso a `users`.
	userRepo := newUserRepository(db)
	isDevelopment := cfg.IsDevelopment()
	// Sender único, compartilhado por todo mundo que "manda e-mail" (convite, redefinição de
	// senha) — hoje só loga (sem SMTP configurado ainda, ver pkg/mailer.LogSender).
	sender := mailer.LogSender{IncludeBody: isDevelopment}
	// CORSOrigin já é a origem do front (http://localhost:4200 em dev) — reaproveitada como base
	// dos links de convite/redefinição de senha, sem precisar de uma env var nova. Só a PRIMEIRA
	// origem: CORS_ORIGIN aceita lista separada por vírgula (ver .env.example), e usar a string
	// crua produziria links com dois hosts grudados ("https://a.example,https://b.example/...") —
	// ninguém conseguiria redefinir senha nem aceitar convite.
	frontendBaseURL := strings.TrimSpace(strings.Split(cfg.CORSOrigin, ",")[0])
	// Extractor/Assessor únicos, compartilhados só pelo domínio candidate (é o único que lê
	// currículo e avalia candidato). Quem decide o provedor é LLM_PROVIDER — ver newLLM.
	extractor, assessor := newLLM(cfg)

	candidateHandler, candidateService := InitCandidateFactory(db, extractor, assessor)

	return &Factory{
		AuthHandler:      InitAuthFactory(db, userRepo, cfg.JWTSecret, !isDevelopment, isDevelopment, frontendBaseURL, sender),
		CompanyHandler:   InitCompanyFactory(db, userRepo),
		UserHandler:      InitUserFactory(userRepo, sender, frontendBaseURL, isDevelopment),
		CampaignHandler:  InitCampaignFactory(db),
		CandidateHandler: candidateHandler,
		TalentHandler:    InitTalentFactory(db, candidateService),
		DashboardHandler: InitDashboardFactory(db),
		ActivityHandler:  InitActivityFactory(db),
		IsUserActive:     userRepo.IsActive,
	}
}
