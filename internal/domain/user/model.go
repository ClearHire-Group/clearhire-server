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
