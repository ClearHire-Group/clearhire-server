package user

import "time"

type Role string

const (
	RoleOwner  Role = "owner"
	RoleMember Role = "member"
)

// User é uma conta de RH dentro de uma empresa (ver db/schema.sql, tabela
// users) — no máximo `seat_limit` por empresa, hoje 2.
type User struct {
	ID           string
	CompanyID    string
	Name         string
	Email        string
	PasswordHash string
	Role         Role
	IsActive     bool
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type InvitationStatus string

const (
	InvitationPending  InvitationStatus = "pending"
	InvitationAccepted InvitationStatus = "accepted"
	InvitationExpired  InvitationStatus = "expired"
	InvitationRevoked  InvitationStatus = "revoked"
)

// Invitation é uma linha de user_invitations — o owner convida o segundo assento de RH, que usa
// o token pra criar a própria senha e virar um User com role=member (ver auth.Service.AcceptInvitation).
type Invitation struct {
	ID              string
	CompanyID       string
	Email           string
	InvitedByUserID string
	TokenHash       string
	Status          InvitationStatus
	ExpiresAt       time.Time
	AcceptedAt      *time.Time
	CreatedAt       time.Time
}

// TeamMember é a visão que a tela Equipe consome — User ativo/inativo e Invitation pendente
// projetados na mesma forma, porque pra quem olha a lista da empresa os dois são "um assento".
type TeamMemberStatus string

const (
	TeamMemberActive   TeamMemberStatus = "active"
	TeamMemberInactive TeamMemberStatus = "inactive"
	TeamMemberPending  TeamMemberStatus = "pending"
)

type TeamMember struct {
	ID        string
	Name      string
	Email     string
	Role      Role
	Status    TeamMemberStatus
	CreatedAt time.Time
}
