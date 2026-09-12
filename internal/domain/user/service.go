package user

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/mailer"
)

const invitationTTL = 7 * 24 * time.Hour

type Service interface {
	Me(ctx context.Context, companyID, userID string) (*User, error)
	UpdateMe(ctx context.Context, companyID, userID string, req UpdateMeRequest) error
	ListTeam(ctx context.Context, companyID string) ([]TeamMember, error)
	// Invite devolve o link de convite cru — só o handler decide se ele vai pra resposta HTTP
	// (development) ou fica só no e-mail enviado via mailer.Sender.
	Invite(ctx context.Context, companyID, invitedByUserID string, req InviteUserRequest) (string, error)
	// Deactivate nunca deixa callerID desativar a própria conta (companyID + targetID escopam
	// a linha, callerID é só a checagem de "não é você mesmo"). callerPassword reprova a senha do
	// owner autenticado antes de executar — o JWT sozinho não basta pra uma ação destrutiva sobre
	// outra conta.
	Deactivate(ctx context.Context, companyID, targetID, callerID, callerPassword string) error
	// CancelInvitation revoga um convite ainda pendente — o link já enviado/exposto vira
	// permanentemente inválido (AcceptInvitation recusa qualquer status != pending). companyID
	// escopa a linha; cancelar um convite de outra empresa ou já aceito/expirado/revogado devolve
	// NotFound, nunca afeta nada.
	CancelInvitation(ctx context.Context, companyID, invitationID string) error
	// ResendInvitation troca um convite pendente por um novo — o token antigo é revogado no mesmo
	// golpe, nunca fica reutilizável em paralelo com o novo. Não existe "reexibir" o link original:
	// só o hash do token fica no banco (mesma lógica de senha), então o único jeito de o owner ter
	// um link funcionando de novo é gerar um convite novo.
	ResendInvitation(ctx context.Context, companyID, invitedByUserID, invitationID string) (string, error)
}

type service struct {
	repo            Repository
	mailer          mailer.Sender
	frontendBaseURL string
}

func NewService(repo Repository, sender mailer.Sender, frontendBaseURL string) Service {
	return &service{repo: repo, mailer: sender, frontendBaseURL: frontendBaseURL}
}

func (s *service) Me(ctx context.Context, companyID, userID string) (*User, error) {
	u, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar usuário")
	}
	if u == nil || u.CompanyID != companyID {
		return nil, apperror.NotFound("usuário não encontrado")
	}
	return u, nil
}

func (s *service) UpdateMe(ctx context.Context, companyID, userID string, req UpdateMeRequest) error {
	matched, err := s.repo.UpdateName(ctx, userID, companyID, req.Name)
	if err != nil {
		return apperror.Internal("falha ao atualizar perfil")
	}
	if !matched {
		return apperror.NotFound("usuário não encontrado")
	}
	return nil
}

func (s *service) ListTeam(ctx context.Context, companyID string) ([]TeamMember, error) {
	users, err := s.repo.ListByCompany(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar equipe")
	}
	invitations, err := s.repo.ListPendingByCompany(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar convites pendentes")
	}

	members := make([]TeamMember, 0, len(users)+len(invitations))
	for _, u := range users {
		status := TeamMemberActive
		if !u.IsActive {
			status = TeamMemberInactive
		}
		members = append(members, TeamMember{ID: u.ID, Name: u.Name, Email: u.Email, Role: u.Role, Status: status, CreatedAt: u.CreatedAt})
	}
	for _, inv := range invitations {
		members = append(members, TeamMember{ID: inv.ID, Name: "", Email: inv.Email, Role: RoleMember, Status: TeamMemberPending, CreatedAt: inv.CreatedAt})
	}
	return members, nil
}

func (s *service) Invite(ctx context.Context, companyID, invitedByUserID string, req InviteUserRequest) (string, error) {
	if existing, err := s.repo.FindByEmail(ctx, req.Email); err == nil && existing != nil {
		return "", apperror.BadRequest("já existe uma conta com este e-mail")
	}
	if pending, err := s.repo.FindPendingInvitationByEmail(ctx, companyID, req.Email); err == nil && pending != nil {
		return "", apperror.BadRequest("já existe um convite pendente para este e-mail")
	}
	return s.createInvitation(ctx, companyID, invitedByUserID, req.Email)
}

func (s *service) ResendInvitation(ctx context.Context, companyID, invitedByUserID, invitationID string) (string, error) {
	inv, err := s.repo.FindInvitationByID(ctx, invitationID, companyID)
	if err != nil {
		return "", apperror.Internal("falha ao buscar convite")
	}
	if inv == nil || inv.Status != InvitationPending {
		return "", apperror.NotFound("convite não encontrado ou já processado")
	}

	// Revoga o convite atual ANTES de criar o novo: um e-mail só pode ter um convite pending por
	// vez (índice único parcial no banco — ver migrations/0001_init.sql), então criar o novo
	// primeiro esbarraria nessa constraint com o antigo ainda pending.
	matched, err := s.repo.RevokeInvitation(ctx, invitationID, companyID)
	if err != nil {
		return "", apperror.Internal("falha ao invalidar convite antigo")
	}
	if !matched {
		// Corrida rara: o convite deixou de ser pending entre o FindInvitationByID acima e aqui
		// (ex.: cancelado em outra aba no mesmo instante) — mesmo erro genérico de "já processado".
		return "", apperror.NotFound("convite não encontrado ou já processado")
	}

	return s.createInvitation(ctx, companyID, invitedByUserID, inv.Email)
}

// createInvitation gera o token opaco, grava o convite pending e dispara o e-mail (melhor
// esforço) — compartilhado por Invite (convite novo) e ResendInvitation (troca o convite pending
// por um com token novo, já que o token antigo nunca pode ser reexibido — só o hash fica salvo).
func (s *service) createInvitation(ctx context.Context, companyID, invitedByUserID, email string) (string, error) {
	rawToken, hash, err := generateOpaqueToken()
	if err != nil {
		return "", apperror.Internal("falha ao gerar convite")
	}

	inv := &Invitation{
		CompanyID:       companyID,
		Email:           email,
		InvitedByUserID: invitedByUserID,
		TokenHash:       hash,
		Status:          InvitationPending,
		ExpiresAt:       time.Now().Add(invitationTTL),
	}
	if err := s.repo.CreateInvitation(ctx, inv); err != nil {
		return "", apperror.Internal("falha ao criar convite")
	}

	link := fmt.Sprintf("%s/aceitar-convite/%s", s.frontendBaseURL, rawToken)
	subject := "Você foi convidado para o Clearhire"
	body := fmt.Sprintf("Você foi convidado a entrar como RH no Clearhire. Acesse o link para criar sua senha: %s", link)
	// Melhor esforço: falha ao "enviar" o e-mail não desfaz o convite já criado — a pessoa
	// convidada ainda pode receber o link por outro canal (ver InviteResponse.InviteLink em dev).
	_ = s.mailer.Send(ctx, email, subject, body)

	return link, nil
}

func (s *service) Deactivate(ctx context.Context, companyID, targetID, callerID, callerPassword string) error {
	if targetID == callerID {
		return apperror.BadRequest("não é possível desativar sua própria conta")
	}

	caller, err := s.repo.FindByID(ctx, callerID)
	if err != nil {
		return apperror.Internal("falha ao validar senha")
	}
	// Mesma mensagem tanto pra conta do caller sumida (não deveria acontecer com um JWT válido)
	// quanto senha errada — não dar pista nenhuma sobre por que a confirmação falhou.
	if caller == nil || bcrypt.CompareHashAndPassword([]byte(caller.PasswordHash), []byte(callerPassword)) != nil {
		return apperror.Unauthorized("senha incorreta")
	}

	matched, err := s.repo.SetActive(ctx, targetID, companyID, false)
	if err != nil {
		return apperror.Internal("falha ao desativar usuário")
	}
	if !matched {
		return apperror.NotFound("usuário não encontrado")
	}

	// Corta o acesso na hora — sem isso o access token do assento removido (até 15min de vida)
	// continua funcionando normalmente, já que middleware.Auth só confere assinatura/expiração.
	// Melhor esforço: a desativação já aconteceu, uma falha aqui não deve virar erro pro caller.
	_ = s.repo.RevokeSessions(ctx, targetID)

	return nil
}

func (s *service) CancelInvitation(ctx context.Context, companyID, invitationID string) error {
	matched, err := s.repo.RevokeInvitation(ctx, invitationID, companyID)
	if err != nil {
		return apperror.Internal("falha ao cancelar convite")
	}
	if !matched {
		return apperror.NotFound("convite não encontrado ou já processado")
	}
	return nil
}

// generateOpaqueToken segue o mesmo padrão de auth.generateRefreshToken — token opaco de 32
// bytes, hash sha256 é o que fica no banco. Duplicado aqui de propósito: pkg/token só cuida do
// JWT de sessão, cada domínio que precisa de um segredo opaco (convite, refresh, reset de senha)
// gera o seu.
func generateOpaqueToken() (raw string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(sum[:]), nil
}
