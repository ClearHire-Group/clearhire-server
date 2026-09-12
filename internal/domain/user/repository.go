package user

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
)

type Repository interface {
	FindByID(ctx context.Context, id string) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	CountActiveByCompany(ctx context.Context, companyID string) (int, error)
	Create(ctx context.Context, u *User) error
	// ListByCompany devolve todo o roster da empresa (ativo e inativo) — a tela Equipe é quem
	// decide como exibir cada status, o repository só lê.
	ListByCompany(ctx context.Context, companyID string) ([]User, error)
	// SetActive e UpdateName são sempre escopados por companyID — nunca confiar num :id de rota
	// sozinho pra mexer na linha de outra empresa (ver CLAUDE.md do server, seção multi-tenancy).
	// O bool devolvido diz se alguma linha bateu o filtro (id + company_id); handler usa isso pra
	// decidir 404 vs sucesso.
	SetActive(ctx context.Context, id, companyID string, active bool) (bool, error)
	UpdateName(ctx context.Context, id, companyID, name string) (bool, error)
	UpdatePasswordHash(ctx context.Context, id, passwordHash string) error
	// RevokeSessions mata os refresh tokens ativos do usuário — chamado ao desativar um assento,
	// pra cortar o acesso na hora em vez de esperar o access token (até 15min) expirar sozinho.
	// Duplicado de auth.Repository.RevokeAllForUser de propósito: auth já importa user (login,
	// convite), então user não pode importar auth de volta (ciclo) — mesmo motivo de
	// generateOpaqueToken estar duplicado entre os dois pacotes.
	RevokeSessions(ctx context.Context, userID string) error

	CreateInvitation(ctx context.Context, inv *Invitation) error
	FindPendingInvitationByEmail(ctx context.Context, companyID, email string) (*Invitation, error)
	FindInvitationByTokenHash(ctx context.Context, tokenHash string) (*Invitation, error)
	MarkInvitationAccepted(ctx context.Context, id string) error
	ListPendingByCompany(ctx context.Context, companyID string) ([]Invitation, error)
}

type postgresRepository struct {
	db database.DB
}

func NewRepository(db database.DB) Repository {
	return &postgresRepository{db: db}
}

const userColumns = "id, company_id, name, email, password_hash, role, is_active, last_login_at, created_at, updated_at"

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.CompanyID, &u.Name, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (r *postgresRepository) FindByID(ctx context.Context, id string) (*User, error) {
	row := r.db.QueryRow(ctx, "select "+userColumns+" from users where id = $1", id)
	return scanUser(row)
}

func (r *postgresRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
	// `email` é citext no banco — a comparação já é case-insensitive sem esforço aqui.
	row := r.db.QueryRow(ctx, "select "+userColumns+" from users where email = $1", email)
	return scanUser(row)
}

func (r *postgresRepository) CountActiveByCompany(ctx context.Context, companyID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, "select count(*) from users where company_id = $1 and is_active", companyID).Scan(&count)
	return count, err
}

func (r *postgresRepository) Create(ctx context.Context, u *User) error {
	// O trigger trg_users_seat_limit no banco é quem garante o limite de
	// assentos de verdade — este insert só falha se ele disparar.
	row := r.db.QueryRow(ctx, `
		insert into users (company_id, name, email, password_hash, role)
		values ($1, $2, $3, $4, $5)
		returning id, created_at, updated_at
	`, u.CompanyID, u.Name, u.Email, u.PasswordHash, u.Role)
	return row.Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
}

func (r *postgresRepository) ListByCompany(ctx context.Context, companyID string) ([]User, error) {
	rows, err := r.db.Query(ctx, "select "+userColumns+" from users where company_id = $1 order by created_at asc", companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (r *postgresRepository) SetActive(ctx context.Context, id, companyID string, active bool) (bool, error) {
	tag, err := r.db.Exec(ctx, "update users set is_active = $3 where id = $1 and company_id = $2", id, companyID, active)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *postgresRepository) UpdateName(ctx context.Context, id, companyID, name string) (bool, error) {
	tag, err := r.db.Exec(ctx, "update users set name = $3 where id = $1 and company_id = $2", id, companyID, name)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *postgresRepository) UpdatePasswordHash(ctx context.Context, id, passwordHash string) error {
	_, err := r.db.Exec(ctx, "update users set password_hash = $2 where id = $1", id, passwordHash)
	return err
}

func (r *postgresRepository) RevokeSessions(ctx context.Context, userID string) error {
	_, err := r.db.Exec(ctx, "update refresh_tokens set revoked_at = now() where user_id = $1 and revoked_at is null", userID)
	return err
}

const invitationColumns = "id, company_id, email, invited_by_user_id, token_hash, status, expires_at, accepted_at, created_at"

func scanInvitation(row pgx.Row) (*Invitation, error) {
	var inv Invitation
	err := row.Scan(&inv.ID, &inv.CompanyID, &inv.Email, &inv.InvitedByUserID, &inv.TokenHash, &inv.Status, &inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &inv, nil
}

func (r *postgresRepository) CreateInvitation(ctx context.Context, inv *Invitation) error {
	row := r.db.QueryRow(ctx, `
		insert into user_invitations (company_id, email, invited_by_user_id, token_hash, status, expires_at)
		values ($1, $2, $3, $4, $5, $6)
		returning id, created_at
	`, inv.CompanyID, inv.Email, inv.InvitedByUserID, inv.TokenHash, inv.Status, inv.ExpiresAt)
	return row.Scan(&inv.ID, &inv.CreatedAt)
}

func (r *postgresRepository) FindPendingInvitationByEmail(ctx context.Context, companyID, email string) (*Invitation, error) {
	row := r.db.QueryRow(ctx, `
		select `+invitationColumns+`
		from user_invitations
		where company_id = $1 and email = $2 and status = 'pending'
	`, companyID, email)
	return scanInvitation(row)
}

func (r *postgresRepository) FindInvitationByTokenHash(ctx context.Context, tokenHash string) (*Invitation, error) {
	row := r.db.QueryRow(ctx, "select "+invitationColumns+" from user_invitations where token_hash = $1", tokenHash)
	return scanInvitation(row)
}

func (r *postgresRepository) MarkInvitationAccepted(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, "update user_invitations set status = 'accepted', accepted_at = now() where id = $1", id)
	return err
}

func (r *postgresRepository) ListPendingByCompany(ctx context.Context, companyID string) ([]Invitation, error) {
	rows, err := r.db.Query(ctx, `
		select `+invitationColumns+`
		from user_invitations
		where company_id = $1 and status = 'pending' and expires_at > now()
		order by created_at asc
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invitations []Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		invitations = append(invitations, *inv)
	}
	return invitations, rows.Err()
}
