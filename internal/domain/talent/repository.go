package talent

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
)

// Repository só LÊ o banco de talentos (mais a transição de consentimento do primeiro contato). Toda
// escrita de perfil — candidatura, reprovação, aprovação, cadastro manual — mora no domínio candidate,
// que é onde a extração e a resolução de skills acontecem.
//
// "Estar no banco" = bank_entered_at preenchido (migrations/0012). Registros de pessoas que só se
// candidataram e ainda não tiveram desfecho existem, mas não aparecem aqui.
type Repository interface {
	ListInBank(ctx context.Context, companyID string) ([]Talent, error)
	FindInBank(ctx context.Context, companyID, id string) (*Talent, error)
	// MarkFirstContact promove "não notificado" para "notificado" (seção 5.2). Devolve false quando o
	// talento não existe no banco desta empresa.
	MarkFirstContact(ctx context.Context, companyID, id string) (bool, error)
	Coverage(ctx context.Context, companyID string) ([]CoverageEntry, error)
}

type postgresRepository struct {
	db database.DB
}

func NewRepository(db database.DB) Repository {
	return &postgresRepository{db: db}
}

const talentColumns = `id, name, coalesce(email::text, ''), coalesce(linkedin_url, ''), coalesce(city, ''), coalesce(state, ''),
	coalesce(modality, ''), coalesce(seniority, ''), years_experience, salary_min::float8, salary_max::float8,
	available_from, coalesce(availability_note, ''), origin::text, legal_basis::text, consent_state::text,
	consent_date, coalesce(summary, ''), coalesce(recruiter_notes, ''), coalesce(education_degree, ''),
	coalesce(education_institution, ''), coalesce(education_period, ''), profile_reviewed_at`

const inBank = `bank_entered_at is not null and deleted_at is null`

func scanTalent(row pgx.Row) (*Talent, error) {
	var t Talent
	err := row.Scan(&t.ID, &t.Name, &t.Email, &t.LinkedInURL, &t.City, &t.State, &t.Modality, &t.Seniority,
		&t.YearsExperience, &t.SalaryMin, &t.SalaryMax, &t.AvailableFrom, &t.AvailabilityNote,
		&t.Origin, &t.LegalBasis, &t.ConsentState, &t.ConsentDate, &t.Summary, &t.RecruiterNotes,
		&t.EducationDegree, &t.EducationInstitution, &t.EducationPeriod, &t.ProfileReviewedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *postgresRepository) ListInBank(ctx context.Context, companyID string) ([]Talent, error) {
	rows, err := r.db.Query(ctx, `select `+talentColumns+` from talents
		where company_id = $1 and `+inBank+` order by bank_entered_at desc`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	talents := []Talent{}
	for rows.Next() {
		t, err := scanTalent(rows)
		if err != nil {
			return nil, err
		}
		talents = append(talents, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Listagem é resumo: sem descrição de experiência e sem histórico — a tabela não mostra nenhum dos
	// dois e eles eram ~metade do tamanho da resposta. O perfil completo vem de FindInBank.
	if err := r.loadDetails(ctx, talents, false); err != nil {
		return nil, err
	}
	return talents, nil
}

func (r *postgresRepository) FindInBank(ctx context.Context, companyID, id string) (*Talent, error) {
	t, err := scanTalent(r.db.QueryRow(ctx, `select `+talentColumns+` from talents
		where id = $1 and company_id = $2 and `+inBank, id, companyID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	list := []Talent{*t}
	if err := r.loadDetails(ctx, list, true); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// loadDetails carrega skills, idiomas, setores, experiência e histórico de TODOS os talentos com uma
// consulta por tipo (não uma por talento): a listagem do banco mostra skills de cada linha, e N+1
// consultas cresceriam com o tamanho do banco.
//
// full=false (listagem) deixa de fora a descrição das experiências e o histórico.
func (r *postgresRepository) loadDetails(ctx context.Context, talents []Talent, full bool) error {
	if len(talents) == 0 {
		return nil
	}
	ids := make([]string, len(talents))
	index := make(map[string]*Talent, len(talents))
	for i := range talents {
		ids[i] = talents[i].ID
		index[talents[i].ID] = &talents[i]
		talents[i].Skills, talents[i].Languages, talents[i].Sectors = []Skill{}, []Language{}, []string{}
		talents[i].Experience, talents[i].History = []ExperienceEntry{}, []HistoryEntry{}
	}

	type loader struct {
		sql  string
		scan func(rows pgx.Rows) error
		// passFull: a consulta recebe `full` como $2.
		passFull bool
	}
	loaders := []loader{
		{`select ts.talent_id, s.canonical_term::text, coalesce(ts.level, ''), ts.years_experience
		  from talent_skills ts join skills s on s.id = ts.skill_id
		  where ts.talent_id = any($1) order by s.canonical_term`,
			func(rows pgx.Rows) error {
				var id string
				var s Skill
				if err := rows.Scan(&id, &s.Term, &s.Level, &s.YearsExperience); err != nil {
					return err
				}
				index[id].Skills = append(index[id].Skills, s)
				return nil
			}, false},
		{`select tl.talent_id, l.name::text, coalesce(tl.proficiency, '')
		  from talent_languages tl join languages l on l.id = tl.language_id
		  where tl.talent_id = any($1) order by l.name`,
			func(rows pgx.Rows) error {
				var id string
				var l Language
				if err := rows.Scan(&id, &l.Name, &l.Proficiency); err != nil {
					return err
				}
				index[id].Languages = append(index[id].Languages, l)
				return nil
			}, false},
		{`select ts.talent_id, s.name::text from talent_sectors ts join sectors s on s.id = ts.sector_id
		  where ts.talent_id = any($1) order by s.name`,
			func(rows pgx.Rows) error {
				var id, name string
				if err := rows.Scan(&id, &name); err != nil {
					return err
				}
				index[id].Sectors = append(index[id].Sectors, name)
				return nil
			}, false},
		{`select talent_id, role, company, period_label, case when $2 then coalesce(description, '') else '' end
		  from talent_experience_entries where talent_id = any($1) order by position`,
			func(rows pgx.Rows) error {
				var id string
				var e ExperienceEntry
				if err := rows.Scan(&id, &e.Role, &e.Company, &e.PeriodLabel, &e.Description); err != nil {
					return err
				}
				index[id].Experience = append(index[id].Experience, e)
				return nil
			}, true},
		{`select c.talent_id, c.campaign_id, camp.title, c.phase_key::text, c.status::text, c.rejection_reason_key
		  from candidates c join campaigns camp on camp.id = c.campaign_id
		  where c.talent_id = any($1) and c.deleted_at is null order by c.created_at desc`,
			func(rows pgx.Rows) error {
				var id string
				var h HistoryEntry
				if err := rows.Scan(&id, &h.CampaignID, &h.CampaignTitle, &h.PhaseKey, &h.Status, &h.RejectionReasonKey); err != nil {
					return err
				}
				index[id].History = append(index[id].History, h)
				return nil
			}, false},
	}
	if !full {
		loaders = loaders[:len(loaders)-1] // o histórico é o último
	}
	for _, l := range loaders {
		args := []any{ids}
		if l.passFull {
			args = append(args, full)
		}
		rows, err := r.db.Query(ctx, l.sql, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			if err := l.scan(rows); err != nil {
				rows.Close()
				return err
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (r *postgresRepository) MarkFirstContact(ctx context.Context, companyID, id string) (bool, error) {
	// Só "não notificado" muda. Quem já consentiu ou já foi notificado continua igual, e quem pediu
	// exclusão nunca volta a ser contatável por aqui — mas os três casos contam como "existe".
	tag, err := r.db.Exec(ctx, `
		update talents set
			consent_state = case when consent_state = 'nao_notificado' then 'notificado'::consent_state else consent_state end,
			profile_reviewed_at = case when consent_state = 'nao_notificado' then now() else profile_reviewed_at end
		where id = $1 and company_id = $2 and `+inBank, id, companyID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *postgresRepository) Coverage(ctx context.Context, companyID string) ([]CoverageEntry, error) {
	// Quem pediu exclusão não conta: o mapa responde "quantas pessoas eu posso chamar com esta skill".
	rows, err := r.db.Query(ctx, `
		select s.canonical_term::text, count(distinct t.id)
		from talents t
		join talent_skills ts on ts.talent_id = t.id
		join skills s on s.id = ts.skill_id
		where t.company_id = $1 and t.bank_entered_at is not null and t.deleted_at is null
		  and t.consent_state <> 'oposicao_exclusao'
		group by s.canonical_term
		order by count(distinct t.id) desc, s.canonical_term
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CoverageEntry{}
	for rows.Next() {
		var e CoverageEntry
		if err := rows.Scan(&e.SkillTerm, &e.Count); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
