package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ClearHire-Group/clearhire-server/internal/domain/user"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/token"
)

const refreshTokenTTL = 30 * 24 * time.Hour

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
	AcceptInvitation(ctx context.Context, invitationToken string, req AcceptInvitationRequest) error
}

type service struct {
	repo      Repository
	users     user.Repository
	jwtSecret string
}

func NewService(repo Repository, users user.Repository, jwtSecret string) Service {
	return &service{repo: repo, users: users, jwtSecret: jwtSecret}
}

func (s *service) Login(ctx context.Context, req LoginRequest) (*TokenPair, error) {
	u, err := s.users.FindByEmail(ctx, req.Email)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar usuário")
	}
	// Mesma mensagem de erro pra "não existe" e "senha errada" — não dar
	// pista de qual e-mail está cadastrado.
	invalid := apperror.Unauthorized("e-mail ou senha inválidos")
	if u == nil || !u.IsActive {
		return nil, invalid
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
		return nil, invalid
	}

	return s.issueSession(ctx, u.ID, u.CompanyID, string(u.Role))
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

func (s *service) AcceptInvitation(ctx context.Context, invitationToken string, req AcceptInvitationRequest) error {
	// TODO: validar token de user_invitations (status=pending, não expirado),
	// criar o segundo usuário (role=member) e marcar o convite como aceito.
	return apperror.Internal("não implementado")
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

	refreshToken, refreshHash, err := generateRefreshToken()
	if err != nil {
		return nil, apperror.Internal("falha ao emitir refresh token")
	}
	expiresAt := time.Now().Add(refreshTokenTTL)
	if err := s.repo.StoreRefreshToken(ctx, userID, refreshHash, expiresAt); err != nil {
		return nil, apperror.Internal("falha ao registrar sessão")
	}

	return &TokenPair{AccessToken: accessToken, RefreshToken: refreshToken, RefreshExpiresAt: expiresAt}, nil
}

// generateRefreshToken devolve o token que vai pro cliente e o hash que fica
// no banco — nunca guardamos o token em texto puro (mesma lógica de senha).
func generateRefreshToken() (raw string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(buf)
	return raw, hashToken(raw), nil
}

// hashToken aplica o mesmo hash usado ao gerar um refresh token novo —
// Refresh/Logout precisam dele pra transformar o valor bruto do cookie na
// chave de busca em refresh_tokens.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
