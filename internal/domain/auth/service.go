package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/user"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/mailer"
	"github.com/ClearHire-Group/clearhire-server/pkg/token"
)

const (
	refreshTokenTTL       = 30 * 24 * time.Hour
	passwordResetTTL      = time.Hour
	seatLimitCheckErrCode = "23514" // check_violation — trigger trg_users_seat_limit no banco
	// uniqueViolationErrCode dispara quando o e-mail do convite já pertence a uma conta de OUTRA
	// empresa (users.email é unique globalmente) — caso legítimo desde que user.Service.Invite
	// parou de recusar isso na hora do convite pra não vazar existência de conta entre empresas
	// (ver comentário lá). Aqui na aceitação é onde isso realmente se resolve.
	uniqueViolationErrCode = "23505"
)

// Service concentra a lógica de autenticação — verificação de senha, emissão
// de token, aceite de convite. O handler não conhece nenhum desses detalhes,
// só chama o Service.
type Service interface {
	Login(ctx context.Context, req LoginRequest) (*TokenPair, error)
	// Refresh troca um refresh token válido por um par novo, revogando o
	// antigo (rotação). Reapresentar um token já revogado é tratado como
	// sinal de comprometimento: mata TODOS os tokens do usuário, não só o
	// apresentado.
	Refresh(ctx context.Context, rawToken string) (*TokenPair, error)
	// Logout é melhor esforço — token vazio, já revogado, ou qualquer erro
	// de banco nunca falha a chamada; o objetivo é sempre limpar o cookie
	// no handler, sessão inválida no servidor ou não.
	Logout(ctx context.Context, rawToken string) error
	// AcceptInvitation devolve o e-mail do convite aceito, pra o front
	// pré-preencher o login (não loga automaticamente).
	AcceptInvitation(ctx context.Context, invitationToken string, req AcceptInvitationRequest) (string, error)
	// RequestPasswordReset nunca falha por "e-mail não encontrado" — devolve link vazio nesse
	// caso, sem erro, pra nunca revelar se o e-mail existe (mesma filosofia de Login).
	RequestPasswordReset(ctx context.Context, email string) (string, error)
	ConfirmPasswordReset(ctx context.Context, rawToken, newPassword string) error
}

type service struct {
	repo            Repository
	users           user.Repository
	jwtSecret       string
	withTx          func(ctx context.Context, fn func(db database.DB) error) error
	mailer          mailer.Sender
	frontendBaseURL string
}

func NewService(
	repo Repository,
	users user.Repository,
	jwtSecret string,
	withTx func(ctx context.Context, fn func(db database.DB) error) error,
	sender mailer.Sender,
	frontendBaseURL string,
) Service {
	return &service{repo: repo, users: users, jwtSecret: jwtSecret, withTx: withTx, mailer: sender, frontendBaseURL: frontendBaseURL}
}

func (s *service) Login(ctx context.Context, req LoginRequest) (*TokenPair, error) {
	u, err := s.users.FindByEmail(ctx, req.Email)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar usuário")
	}
	// Mesma mensagem de erro pra "não existe" e "senha errada" — não dar
	// pista de qual e-mail está cadastrado.
	invalid := apperror.Unauthorized("e-mail ou senha inválidos")

	// bcrypt.CompareHashAndPassword roda SEMPRE, mesmo pra e-mail inexistente/inativo — contra um
	// hash dummy fixo nesse caso. Sem isso, só a conta que existe de verdade pagava o custo do
	// bcrypt (~150-200ms), e essa diferença de tempo é um oráculo limpo de enumeração de e-mail
	// (achado num pentest: e-mail inexistente respondia em ~3ms, e-mail real+senha errada em
	// ~177ms — a mensagem de erro era idêntica, mas o timing já entregava a resposta).
	hash := dummyPasswordHash
	found := u != nil && u.IsActive
	if found {
		hash = u.PasswordHash
	}
	pwErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password))
	if !found || pwErr != nil {
		return nil, invalid
	}

	return s.issueSession(ctx, u.ID, u.CompanyID, string(u.Role))
}

// dummyPasswordHash é comparado no lugar do hash real quando o e-mail não corresponde a uma conta
// ativa — gerado uma vez no boot (custo pago uma única vez), nunca corresponde a nenhuma senha de
// verdade, só existe pra o bcrypt.CompareHashAndPassword ter o mesmo custo em qualquer branch de
// Login.
var dummyPasswordHash = mustHashDummyPassword()

func mustHashDummyPassword() string {
	hash, err := bcrypt.GenerateFromPassword([]byte("nunca-corresponde-a-senha-nenhuma"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(hash)
}

func (s *service) Refresh(ctx context.Context, rawToken string) (*TokenPair, error) {
	invalid := apperror.Unauthorized("sessão inválida ou expirada")
	if rawToken == "" {
		return nil, invalid
	}

	rt, err := s.repo.FindRefreshTokenByHash(ctx, hashToken(rawToken))
	if err != nil {
		return nil, apperror.Internal("falha ao validar sessão")
	}
	if rt == nil {
		return nil, invalid
	}
	if rt.RevokedAt != nil {
		// Token já revogado (rotacionado ou deslogado) sendo reapresentado —
		// não é um erro de usuário, é sinal de token duplicado/roubado.
		// Resposta é a mesma 401 genérica; a diferença é só o efeito colateral.
		_ = s.repo.RevokeAllForUser(ctx, rt.UserID)
		return nil, invalid
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, invalid
	}

	u, err := s.users.FindByID(ctx, rt.UserID)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar usuário")
	}
	if u == nil || !u.IsActive {
		return nil, invalid
	}

	pair, err := s.issueSession(ctx, u.ID, u.CompanyID, string(u.Role))
	if err != nil {
		return nil, err
	}
	// Grava o token novo ANTES de revogar o antigo: se a gravação falhar, o
	// antigo continua válido e o cliente pode tentar de novo — a ordem
	// inversa arriscaria travar o usuário fora por um erro transiente.
	if err := s.repo.RevokeRefreshToken(ctx, rt.TokenHash); err != nil {
		return nil, apperror.Internal("falha ao rotacionar sessão")
	}
	return pair, nil
}

func (s *service) Logout(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	_ = s.repo.RevokeRefreshToken(ctx, hashToken(rawToken))
	return nil
}

func (s *service) AcceptInvitation(ctx context.Context, invitationToken string, req AcceptInvitationRequest) (string, error) {
	invalid := apperror.BadRequest("convite inválido ou expirado")

	inv, err := s.users.FindInvitationByTokenHash(ctx, hashToken(invitationToken))
	if err != nil {
		return "", apperror.Internal("falha ao validar convite")
	}
	if inv == nil || inv.Status != user.InvitationPending || time.Now().After(inv.ExpiresAt) {
		return "", invalid
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return "", apperror.Internal("falha ao processar senha")
	}

	newUser := &user.User{
		CompanyID:    inv.CompanyID,
		Name:         req.Name,
		Email:        inv.Email,
		PasswordHash: string(hash),
		Role:         user.RoleMember,
	}

	// Criar o usuário + marcar o convite aceito numa única transação: uma falha no meio não pode
	// deixar um usuário órfão sem convite marcado, nem um convite "aceito" sem usuário de verdade.
	err = s.withTx(ctx, func(db database.DB) error {
		txUsers := user.NewRepository(db)
		if err := txUsers.Create(ctx, newUser); err != nil {
			return err
		}
		return txUsers.MarkInvitationAccepted(ctx, inv.ID)
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case seatLimitCheckErrCode:
				return "", apperror.BadRequest("a empresa já atingiu o limite de assentos de RH")
			case uniqueViolationErrCode:
				// E-mail já pertence a uma conta de outra empresa — mesma mensagem genérica de
				// convite inválido usada acima, nunca "este e-mail já tem conta em outro lugar":
				// quem está tentando aceitar não tem por que saber disso, e o owner que mandou o
				// convite também não descobriu nada na hora de criar (ver user.Service.Invite).
				return "", invalid
			}
		}
		return "", apperror.Internal("falha ao aceitar convite")
	}

	return inv.Email, nil
}

func (s *service) RequestPasswordReset(ctx context.Context, email string) (string, error) {
	u, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return "", apperror.Internal("falha ao buscar usuário")
	}
	// E-mail inexistente/inativo: sucesso silencioso, sem link — nunca revela se a conta existe.
	if u == nil || !u.IsActive {
		return "", nil
	}

	// Invalida qualquer link de reset anterior ainda não usado antes de emitir o novo — sem isso,
	// um link antigo (dentro da 1h de validade) continuava funcionando em paralelo com o mais
	// recente: quem pedisse reset duas vezes e usasse o link mais velho por engano (ou um
	// atacante que tivesse interceptado um e-mail antigo) conseguia trocar a senha de novo depois,
	// desfazendo silenciosamente a troca que o dono da conta já tinha feito.
	if err := s.repo.InvalidatePendingPasswordResetTokens(ctx, u.ID); err != nil {
		return "", apperror.Internal("falha ao gerar link de redefinição")
	}

	rawToken, hash, err := generateOpaqueToken()
	if err != nil {
		return "", apperror.Internal("falha ao gerar link de redefinição")
	}
	if err := s.repo.CreatePasswordResetToken(ctx, u.ID, hash, time.Now().Add(passwordResetTTL)); err != nil {
		return "", apperror.Internal("falha ao gerar link de redefinição")
	}

	link := fmt.Sprintf("%s/redefinir-senha/%s", s.frontendBaseURL, rawToken)
	body := fmt.Sprintf("Recebemos um pedido de redefinição de senha. Acesse o link para criar uma senha nova: %s", link)
	_ = s.mailer.Send(ctx, u.Email, "Redefinição de senha — Clearhire", body)

	return link, nil
}

func (s *service) ConfirmPasswordReset(ctx context.Context, rawToken, newPassword string) error {
	invalid := apperror.Unauthorized("link inválido ou expirado")

	rt, err := s.repo.FindPasswordResetTokenByHash(ctx, hashToken(rawToken))
	if err != nil {
		return apperror.Internal("falha ao validar link")
	}
	if rt == nil || rt.UsedAt != nil || time.Now().After(rt.ExpiresAt) {
		return invalid
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return apperror.Internal("falha ao processar senha")
	}
	if err := s.users.UpdatePasswordHash(ctx, rt.UserID, string(hash)); err != nil {
		return apperror.Internal("falha ao atualizar senha")
	}
	if err := s.repo.MarkPasswordResetTokenUsed(ctx, rt.ID); err != nil {
		return apperror.Internal("falha ao atualizar senha")
	}
	// Redefinir a senha derruba todas as sessões ativas — mesmo efeito de "sair de todos os
	// dispositivos" já usado quando um refresh token roubado é detectado.
	_ = s.repo.RevokeAllForUser(ctx, rt.UserID)

	return nil
}

// issueSession emite o par access+refresh de um usuário já autenticado —
// compartilhado por Login (senha verificada) e Refresh (refresh token
// verificado), as duas únicas formas de uma sessão nascer/renovar.
func (s *service) issueSession(ctx context.Context, userID, companyID, role string) (*TokenPair, error) {
	accessToken, err := token.Issue(s.jwtSecret, token.Claims{
		UserID:    userID,
		CompanyID: companyID,
		Role:      role,
	})
	if err != nil {
		return nil, apperror.Internal("falha ao emitir token")
	}

	refreshToken, refreshHash, err := generateOpaqueToken()
	if err != nil {
		return nil, apperror.Internal("falha ao emitir refresh token")
	}
	expiresAt := time.Now().Add(refreshTokenTTL)
	if err := s.repo.StoreRefreshToken(ctx, userID, refreshHash, expiresAt); err != nil {
		return nil, apperror.Internal("falha ao registrar sessão")
	}

	return &TokenPair{AccessToken: accessToken, RefreshToken: refreshToken, RefreshExpiresAt: expiresAt}, nil
}

// generateOpaqueToken devolve o token que vai pro cliente e o hash que fica no banco — nunca
// guardamos o token em texto puro (mesma lógica de senha). Usado por refresh token, convite de
// RH e link de redefinição de senha — todo segredo opaco de sessão/convite nasce daqui.
func generateOpaqueToken() (raw string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(buf)
	return raw, hashToken(raw), nil
}

// hashToken aplica o mesmo hash usado ao gerar um token opaco novo —
// Refresh/Logout/AcceptInvitation/ConfirmPasswordReset precisam dele pra transformar o valor
// bruto recebido do cliente na chave de busca no banco.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
