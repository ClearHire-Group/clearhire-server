package candidate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// PhaseRow é uma linha de campaign_phases — só o suficiente pra descobrir a próxima fase ao
// avançar um candidato (mesma fonte de verdade que internal/domain/campaign usa pro funil).
type PhaseRow struct {
	Key      string
	Position int
}

type Repository interface {
	// FindByID devolve o candidato + tudo que a tela de perfil precisa (experiência, educação,
	// skills, avaliação de IA mais recente). nil, nil quando não encontrado/outra empresa.
	FindByID(ctx context.Context, companyID, id string) (*CandidateDetail, error)
	// ListByCampaign devolve a listagem enxuta (sem experiência/educação/skills) usada na tela de
	// campanha. phaseKey vazio lista todas as fases.
	ListByCampaign(ctx context.Context, companyID, campaignID, phaseKey string) ([]Candidate, error)
	// FindPhasesByCampaign devolve as fases configuradas da campanha ordenadas por position —
	// usado pra achar a "próxima fase" ao avançar um candidato.
	FindPhasesByCampaign(ctx context.Context, companyID, campaignID string) ([]PhaseRow, error)
	// FindBasicByID é a versão leve de FindByID usada dentro da transação de Decide — só os campos
	// necessários pra decidir (fase atual, campanha, nome/e-mail pro caso de virar talento).
	FindBasicByID(ctx context.Context, companyID, id string) (*Candidate, error)
	// FindJobContext devolve os campos da vaga que a avaliação de IA usa. nil, nil quando a
	// campanha não existe ou é de outra empresa.
	FindJobContext(ctx context.Context, companyID, campaignID string) (*llm.JobContext, error)
	// FindAssessmentVersion devolve a avaliação já gravada para (candidato, fase, versão do prompt),
	// ou nil, nil — pedir a mesma avaliação de novo é leitura, não chamada paga.
	FindAssessmentVersion(ctx context.Context, candidateID, phaseKey, promptVersion string) (*AIAssessment, error)
	// SaveAssessment grava a avaliação e seus pontos e tira o candidato de 'aguardando_triagem_ia'.
	// Devolve false quando outra requisição já gravou a mesma (candidato, fase, versão) — a segunda
	// perde a corrida sem erro e sem linha duplicada. Roda dentro de uma transação do service.
	SaveAssessment(ctx context.Context, candidateID, phaseKey string, a *llm.Assessment, origin AssessmentOrigin) (bool, error)
	// LatestAssessmentID devolve o id da avaliação de IA mais recente do candidato, ou "" se ainda
	// não foi avaliado — vira o par (recomendação × decisão) registrado em candidate_decisions.
	LatestAssessmentID(ctx context.Context, candidateID string) (string, error)
	// AdvancePhase mata dois coelhos numa Exec só: move o candidato pra nextPhaseKey e já ajusta o
	// status pro que faz sentido na fase nova. Devolve false se o candidato não existe/não é desta
	// empresa (0 linhas afetadas).
	AdvancePhase(ctx context.Context, companyID, id, nextPhaseKey string, nextStatus Status) (bool, error)
	// Reject marca reprovado (status/motivo/rejected_at) e, se talentID != nil, já liga
	// candidates.talent_id na mesma escrita — nunca duas queries pra um estado que tem que nascer
	// junto.
	Reject(ctx context.Context, companyID, id, reasonKey string, talentID *string) (bool, error)
	CreateDecision(ctx context.Context, d *Decision) error
	// CreateTalentFromRejection cria o talento mínimo (sem embedding — ver TalentSeed) e devolve o
	// id gerado, pra Reject linkar em candidates.talent_id.
	CreateTalentFromRejection(ctx context.Context, t *TalentSeed) (string, error)

	// --- Candidatura pública (link de campanha) ---

	// FindPublicCampaignRef reconfirma a elegibilidade da campanha no momento da escrita — nunca
	// confia que quem chamou (o handler, a partir do próprio :id da rota) já validou antes. nil,
	// nil cobre "não existe" e "não elegível" com o mesmo resultado.
	FindPublicCampaignRef(ctx context.Context, campaignID string) (*PublicCampaignRef, error)
	// FindExistingApplication detecta candidatura duplicada — mesma campanha, mesmo e-mail
	// (citext, comparação já é case-insensitive).
	FindExistingApplication(ctx context.Context, campaignID, email string) (bool, error)
	// FindCachedExtraction devolve nil, nil quando não há extração salva para este conteúdo —
	// "não tem" é caminho normal aqui, não erro. Ver migrations/0007.
	FindCachedExtraction(ctx context.Context, companyID, fingerprint string) (*llm.ExtractedProfile, error)
	// SaveExtraction guarda o resultado da extração paga. Idempotente: envio concorrente do mesmo
	// currículo não pode estourar a unique — o segundo simplesmente não sobrescreve.
	SaveExtraction(ctx context.Context, companyID, fingerprint string, profile *llm.ExtractedProfile) error
	// CreateTalentFromPublicApplication grava o perfil rico (extraído pela IA ou preenchido
	// manualmente) — nunca referencia embedding, mesma disciplina de CreateTalentFromRejection.
	CreateTalentFromPublicApplication(ctx context.Context, t *TalentSeed) (string, error)
	CreateCandidate(ctx context.Context, c *CandidateSeed) (string, error)
	InsertCandidateExperience(ctx context.Context, candidateID string, entries []ExperienceEntry) error
	InsertTalentExperience(ctx context.Context, talentID string, entries []ExperienceEntry) error
	// ResolveSkills casa cada termo contra skills.canonical_term ∪ skill_synonyms.synonym_text
	// (match exato, citext já é case-insensitive). O que não bate não vira ResolvedSkill — o
	// service decide se isso vai pra fila de revisão (ver QueueSkillReview).
	ResolveSkills(ctx context.Context, terms []llm.SkillMention) (resolved []ResolvedSkill, unmapped []llm.SkillMention, err error)
	// FindSkillMentionsInText escaneia o texto (case-insensitive) por qualquer termo canônico ou
	// sinônimo já cadastrado na taxonomia — via determinística de achar skill num currículo, sem
	// IA nenhuma. Só encontra o que já está na taxonomia, nunca "quase-match"; por isso, diferente
	// de ResolveSkills, não produz termos "não mapeados" pra fila de revisão.
	FindSkillMentionsInText(ctx context.Context, text string) ([]ResolvedSkill, error)
	InsertCandidateSkills(ctx context.Context, candidateID string, skills []ResolvedSkill) error
	InsertTalentSkills(ctx context.Context, talentID string, skills []ResolvedSkill) error
	// QueueSkillReview registra um termo que a extração não conseguiu mapear pra taxonomia
	// controlada — skill_mapping_review_queue já existia no schema desde o início, nunca usada
	// até agora (o domínio talent sempre foi esqueleto).
	QueueSkillReview(ctx context.Context, companyID, rawTerm string) error
	// InsertTalentSectors/InsertTalentLanguages fazem match exato contra sectors.name/
	// languages.name — o que não bate é descartado (sem fila de revisão pra isso, só skills tem).
	InsertTalentSectors(ctx context.Context, talentID string, names []string) error
	InsertTalentLanguages(ctx context.Context, talentID string, langs []llm.LanguageMention) error
	// InsertTalentSourceDocument grava o texto que a IA efetivamente leu — o rastro de auditoria
	// que substitui guardar o PDF original (nunca gravado em disco).
	InsertTalentSourceDocument(ctx context.Context, talentID, rawText string) error
}

type postgresRepository struct {
	db database.DB
}

func NewRepository(db database.DB) Repository {
	return &postgresRepository{db: db}
}

const candidateColumns = `id, company_id, campaign_id, talent_id, name, coalesce(email, ''),
	coalesce(city, ''), coalesce(state, ''), years_experience, phase_key, status, rejection_reason_key,
	created_at, updated_at`

func scanCandidate(row pgx.Row) (*Candidate, error) {
	var c Candidate
	err := row.Scan(&c.ID, &c.CompanyID, &c.CampaignID, &c.TalentID, &c.Name, &c.Email,
		&c.City, &c.State, &c.YearsExperience, &c.PhaseKey, &c.Status, &c.RejectionReasonKey,
		&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *postgresRepository) FindBasicByID(ctx context.Context, companyID, id string) (*Candidate, error) {
	row := r.db.QueryRow(ctx, "select "+candidateColumns+" from candidates where id = $1 and company_id = $2 and deleted_at is null", id, companyID)
	return scanCandidate(row)
}

func (r *postgresRepository) ListByCampaign(ctx context.Context, companyID, campaignID, phaseKey string) ([]Candidate, error) {
	rows, err := r.db.Query(ctx, `
		select c.id, c.company_id, c.campaign_id, c.talent_id, c.name, coalesce(c.email, ''),
		       coalesce(c.city, ''), coalesce(c.state, ''), c.years_experience, c.phase_key, c.status,
		       c.rejection_reason_key, c.created_at, c.updated_at,
		       a.match_pct
		from candidates c
		left join lateral (
			select match_pct from candidate_ai_assessments
			where candidate_id = c.id order by created_at desc limit 1
		) a on true
		where c.company_id = $1 and c.campaign_id = $2 and c.deleted_at is null
		  -- cast pra text de propósito: phase_key é enum, e comparar enum = '' (parâmetro vazio
		  -- quando não veio ?phase= na URL) estoura erro de tipo no Postgres mesmo dentro de um OR
		  -- — o planner tenta tipar os dois lados da igualdade antes de decidir o short-circuit.
		  and ($3 = '' or c.phase_key::text = $3)
		order by c.created_at asc
	`, companyID, campaignID, phaseKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.ID, &c.CompanyID, &c.CampaignID, &c.TalentID, &c.Name, &c.Email,
			&c.City, &c.State, &c.YearsExperience, &c.PhaseKey, &c.Status, &c.RejectionReasonKey,
			&c.CreatedAt, &c.UpdatedAt, &c.MatchPct); err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

func (r *postgresRepository) FindByID(ctx context.Context, companyID, id string) (*CandidateDetail, error) {
	row := r.db.QueryRow(ctx, `
		select id, company_id, campaign_id, talent_id, name, coalesce(email, ''), coalesce(phone, ''),
		       coalesce(linkedin_url, ''), coalesce(city, ''), coalesce(state, ''), years_experience,
		       phase_key, status, rejection_reason_key, coalesce(summary, ''),
		       coalesce(education_degree, ''), coalesce(education_institution, ''),
		       coalesce(education_period, ''), created_at, updated_at
		from candidates
		where id = $1 and company_id = $2 and deleted_at is null
	`, id, companyID)

	var d CandidateDetail
	err := row.Scan(&d.ID, &d.CompanyID, &d.CampaignID, &d.TalentID, &d.Name, &d.Email, &d.Phone,
		&d.LinkedInURL, &d.City, &d.State, &d.YearsExperience, &d.PhaseKey, &d.Status, &d.RejectionReasonKey,
		&d.Summary, &d.EducationDegree, &d.EducationInstitution, &d.EducationPeriod, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	expRows, err := r.db.Query(ctx, `
		select role, company, period_label, coalesce(description, ''), position
		from candidate_experience_entries where candidate_id = $1 order by position asc
	`, id)
	if err != nil {
		return nil, err
	}
	defer expRows.Close()
	for expRows.Next() {
		var e ExperienceEntry
		if err := expRows.Scan(&e.Role, &e.Company, &e.PeriodLabel, &e.Description, &e.Position); err != nil {
			return nil, err
		}
		d.Experience = append(d.Experience, e)
	}
	if err := expRows.Err(); err != nil {
		return nil, err
	}

	skillRows, err := r.db.Query(ctx, `
		select s.canonical_term from candidate_skills cs
		join skills s on s.id = cs.skill_id
		where cs.candidate_id = $1 order by s.canonical_term asc
	`, id)
	if err != nil {
		return nil, err
	}
	defer skillRows.Close()
	for skillRows.Next() {
		var term string
		if err := skillRows.Scan(&term); err != nil {
			return nil, err
		}
		d.Skills = append(d.Skills, term)
	}
	if err := skillRows.Err(); err != nil {
		return nil, err
	}
	if d.Skills == nil {
		d.Skills = []string{}
	}

	ai, err := r.latestAssessment(ctx, id)
	if err != nil {
		return nil, err
	}
	d.AI = ai

	return &d, nil
}

func (r *postgresRepository) latestAssessment(ctx context.Context, candidateID string) (*AIAssessment, error) {
	return r.queryAssessment(ctx, "where candidate_id = $1 order by created_at desc limit 1", candidateID)
}

// FindAssessmentVersion devolve a avaliação já gravada para (candidato, fase, versão do prompt) —
// é o que torna pedir a mesma avaliação duas vezes uma leitura, não uma segunda chamada paga.
func (r *postgresRepository) FindAssessmentVersion(ctx context.Context, candidateID, phaseKey, promptVersion string) (*AIAssessment, error) {
	return r.queryAssessment(ctx, "where candidate_id = $1 and phase_key::text = $2 and prompt_version = $3 limit 1",
		candidateID, phaseKey, promptVersion)
}

// queryAssessment lê UMA avaliação (e seus pontos) escolhida pela cláusula `where ... limit 1`.
// nil, nil quando não há nenhuma. A cláusula é sempre uma constante deste pacote, nunca dado do
// cliente — os valores vão em args.
func (r *postgresRepository) queryAssessment(ctx context.Context, clause string, args ...any) (*AIAssessment, error) {
	var assessmentID string
	var ai AIAssessment
	var matchLabel, matchNote, justification *string
	row := r.db.QueryRow(ctx, `
		select id, match_pct, match_label, match_note, justification
		from candidate_ai_assessments `+clause, args...)
	err := row.Scan(&assessmentID, &ai.MatchPct, &matchLabel, &matchNote, &justification)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if matchLabel != nil {
		ai.MatchLabel = *matchLabel
	}
	if matchNote != nil {
		ai.MatchNote = *matchNote
	}
	if justification != nil {
		ai.Justification = *justification
	}

	pointRows, err := r.db.Query(ctx, `
		select kind, text from candidate_ai_assessment_points
		where assessment_id = $1 order by position asc
	`, assessmentID)
	if err != nil {
		return nil, err
	}
	defer pointRows.Close()
	for pointRows.Next() {
		var kind, text string
		if err := pointRows.Scan(&kind, &text); err != nil {
			return nil, err
		}
		if kind == "strength" {
			ai.Strengths = append(ai.Strengths, text)
		} else {
			ai.Concerns = append(ai.Concerns, text)
		}
	}
	if err := pointRows.Err(); err != nil {
		return nil, err
	}
	if ai.Strengths == nil {
		ai.Strengths = []string{}
	}
	if ai.Concerns == nil {
		ai.Concerns = []string{}
	}
	return &ai, nil
}

func (r *postgresRepository) FindJobContext(ctx context.Context, companyID, campaignID string) (*llm.JobContext, error) {
	var j llm.JobContext
	err := r.db.QueryRow(ctx, `
		select title, seniority::text, modality::text, coalesce(description, ''),
		       coalesce(responsibilities, ''), coalesce(requirements, '')
		from campaigns where id = $1 and company_id = $2 and deleted_at is null
	`, campaignID, companyID).Scan(&j.Title, &j.Seniority, &j.Modality, &j.Description, &j.Responsibilities, &j.Requirements)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &j, nil
}

func (r *postgresRepository) SaveAssessment(ctx context.Context, candidateID, phaseKey string, a *llm.Assessment, origin AssessmentOrigin) (bool, error) {
	var assessmentID string
	err := r.db.QueryRow(ctx, `
		insert into candidate_ai_assessments
			(candidate_id, phase_key, match_pct, match_label, match_note, justification, provider, model, prompt_version)
		values ($1, $2, $3, nullif($4, ''), nullif($5, ''), nullif($6, ''), $7, $8, $9)
		on conflict (candidate_id, phase_key, prompt_version) where prompt_version is not null do nothing
		returning id
	`, candidateID, phaseKey, a.MatchPct, a.MatchLabel, a.MatchNote, a.Justification,
		origin.Provider, origin.Model, origin.PromptVersion).Scan(&assessmentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // conflito: outra requisição gravou primeiro
		}
		return false, err
	}

	for _, point := range []struct {
		kind  string
		items []string
	}{{"strength", a.Strengths}, {"concern", a.Concerns}} {
		for i, text := range point.items {
			if _, err := r.db.Exec(ctx, `
				insert into candidate_ai_assessment_points (assessment_id, kind, text, position)
				values ($1, $2, $3, $4)
			`, assessmentID, point.kind, text, i+1); err != nil {
				return false, err
			}
		}
	}

	// Só sai de "aguardando triagem": quem já está em análise/decisão não volta atrás por causa de
	// uma reavaliação. Nunca mexe em fase, reprovação ou decisão — a IA sugere, o RH decide.
	if _, err := r.db.Exec(ctx, `
		update candidates set status = 'aguardando_decisao'
		where id = $1 and status = 'aguardando_triagem_ia'
	`, candidateID); err != nil {
		return false, err
	}
	return true, nil
}

func (r *postgresRepository) FindPhasesByCampaign(ctx context.Context, companyID, campaignID string) ([]PhaseRow, error) {
	rows, err := r.db.Query(ctx, `
		select cp.phase_key, cp.position
		from campaign_phases cp
		join campaigns c on c.id = cp.campaign_id
		where cp.campaign_id = $1 and c.company_id = $2
		order by cp.position asc
	`, campaignID, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var phases []PhaseRow
	for rows.Next() {
		var p PhaseRow
		if err := rows.Scan(&p.Key, &p.Position); err != nil {
			return nil, err
		}
		phases = append(phases, p)
	}
	return phases, rows.Err()
}

func (r *postgresRepository) LatestAssessmentID(ctx context.Context, candidateID string) (string, error) {
	var id string
	row := r.db.QueryRow(ctx, `
		select id from candidate_ai_assessments where candidate_id = $1 order by created_at desc limit 1
	`, candidateID)
	err := row.Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return id, nil
}

func (r *postgresRepository) AdvancePhase(ctx context.Context, companyID, id, nextPhaseKey string, nextStatus Status) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		update candidates set phase_key = $3, status = $4
		where id = $1 and company_id = $2 and deleted_at is null
	`, id, companyID, nextPhaseKey, nextStatus)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *postgresRepository) Reject(ctx context.Context, companyID, id, reasonKey string, talentID *string) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		update candidates
		set status = 'reprovado', rejection_reason_key = $3, rejected_at = now(), talent_id = coalesce($4, talent_id)
		where id = $1 and company_id = $2 and deleted_at is null
	`, id, companyID, reasonKey, talentID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *postgresRepository) CreateDecision(ctx context.Context, d *Decision) error {
	_, err := r.db.Exec(ctx, `
		insert into candidate_decisions
			(company_id, candidate_id, decided_by_user_id, assessment_id, decision, from_phase, to_phase, rejection_reason_key)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
	`, d.CompanyID, d.CandidateID, d.DecidedByUserID, d.AssessmentID, d.Decision, d.FromPhase, d.ToPhase, d.RejectionReasonKey)
	return err
}

func (r *postgresRepository) CreateTalentFromRejection(ctx context.Context, t *TalentSeed) (string, error) {
	var id string
	row := r.db.QueryRow(ctx, `
		insert into talents (company_id, name, email, city, state, origin, legal_basis, consent_state, consent_date)
		values ($1, $2, nullif($3, ''), nullif($4, ''), nullif($5, ''), $6, $7, $8, current_date)
		returning id
	`, t.CompanyID, t.Name, t.Email, t.City, t.State, t.Origin, t.LegalBasis, t.ConsentState)
	if err := row.Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

func (r *postgresRepository) FindPublicCampaignRef(ctx context.Context, campaignID string) (*PublicCampaignRef, error) {
	var ref PublicCampaignRef
	row := r.db.QueryRow(ctx, `
		select company_id from campaigns
		where id = $1 and status = 'ativa' and accepts_public_applications and deleted_at is null
	`, campaignID)
	err := row.Scan(&ref.CompanyID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &ref, nil
}

func (r *postgresRepository) FindExistingApplication(ctx context.Context, campaignID, email string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		select exists(select 1 from candidates where campaign_id = $1 and email = $2 and deleted_at is null)
	`, campaignID, email).Scan(&exists)
	return exists, err
}

func (r *postgresRepository) FindCachedExtraction(ctx context.Context, companyID, fingerprint string) (*llm.ExtractedProfile, error) {
	var raw []byte
	err := r.db.QueryRow(ctx, `
		select profile from resume_extractions where company_id = $1 and fingerprint = $2
	`, companyID, fingerprint).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var profile llm.ExtractedProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		// Linha ilegível (formato antigo, escrita corrompida) não pode derrubar a candidatura —
		// vale mais re-extrair e pagar de novo do que recusar a pessoa.
		return nil, nil
	}
	return &profile, nil
}

func (r *postgresRepository) SaveExtraction(ctx context.Context, companyID, fingerprint string, profile *llm.ExtractedProfile) error {
	raw, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		insert into resume_extractions (company_id, fingerprint, profile)
		values ($1, $2, $3)
		on conflict (company_id, fingerprint) do nothing
	`, companyID, fingerprint, raw)
	return err
}

func (r *postgresRepository) CreateTalentFromPublicApplication(ctx context.Context, t *TalentSeed) (string, error) {
	var id string
	row := r.db.QueryRow(ctx, `
		insert into talents (company_id, name, email, phone, city, state, linkedin_url, modality, seniority,
		                      years_experience, salary_min, salary_max, available_from, availability_note,
		                      origin, legal_basis, consent_state, consent_date, summary,
		                      education_degree, education_institution, education_period)
		values ($1, $2, nullif($3, ''), nullif($4, ''), nullif($5, ''), nullif($6, ''), nullif($7, ''),
		        nullif($8, ''), nullif($9, ''), $10, $11, $12, $13, nullif($14, ''),
		        $15, $16, $17, current_date, nullif($18, ''),
		        nullif($19, ''), nullif($20, ''), nullif($21, ''))
		returning id
	`, t.CompanyID, t.Name, t.Email, t.Phone, t.City, t.State, t.LinkedInURL, t.Modality, t.Seniority,
		t.YearsExperience, t.SalaryMin, t.SalaryMax, t.AvailableFrom, t.AvailabilityNote,
		t.Origin, t.LegalBasis, t.ConsentState, t.Summary,
		t.EducationDegree, t.EducationInstitution, t.EducationPeriod)
	if err := row.Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

func (r *postgresRepository) CreateCandidate(ctx context.Context, c *CandidateSeed) (string, error) {
	var id string
	row := r.db.QueryRow(ctx, `
		insert into candidates (company_id, campaign_id, talent_id, name, email, phone, linkedin_url, city, state,
		                         years_experience, summary, education_degree, education_institution, education_period)
		values ($1, $2, $3, $4, nullif($5, ''), nullif($6, ''), nullif($7, ''), nullif($8, ''), nullif($9, ''),
		        $10, nullif($11, ''), nullif($12, ''), nullif($13, ''), nullif($14, ''))
		returning id
	`, c.CompanyID, c.CampaignID, c.TalentID, c.Name, c.Email, c.Phone, c.LinkedInURL, c.City, c.State,
		c.YearsExperience, c.Summary, c.EducationDegree, c.EducationInstitution, c.EducationPeriod)
	if err := row.Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

func (r *postgresRepository) InsertCandidateExperience(ctx context.Context, candidateID string, entries []ExperienceEntry) error {
	for i, e := range entries {
		if _, err := r.db.Exec(ctx, `
			insert into candidate_experience_entries (candidate_id, role, company, period_label, description, position)
			values ($1, $2, $3, $4, nullif($5, ''), $6)
		`, candidateID, e.Role, e.Company, e.PeriodLabel, e.Description, i); err != nil {
			return err
		}
	}
	return nil
}

func (r *postgresRepository) InsertTalentExperience(ctx context.Context, talentID string, entries []ExperienceEntry) error {
	for i, e := range entries {
		if _, err := r.db.Exec(ctx, `
			insert into talent_experience_entries (talent_id, role, company, period_label, description, position)
			values ($1, $2, $3, $4, nullif($5, ''), $6)
		`, talentID, e.Role, e.Company, e.PeriodLabel, e.Description, i); err != nil {
			return err
		}
	}
	return nil
}

func (r *postgresRepository) ResolveSkills(ctx context.Context, terms []llm.SkillMention) ([]ResolvedSkill, []llm.SkillMention, error) {
	var resolved []ResolvedSkill
	var unmapped []llm.SkillMention
	for _, t := range terms {
		if t.Term == "" {
			continue
		}
		var skillID string
		err := r.db.QueryRow(ctx, `
			select id from skills where canonical_term = $1
			union
			select sk.id from skill_synonyms syn join skills sk on sk.id = syn.skill_id where syn.synonym_text = $1
			limit 1
		`, t.Term).Scan(&skillID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				unmapped = append(unmapped, t)
				continue
			}
			return nil, nil, err
		}
		resolved = append(resolved, ResolvedSkill{SkillID: skillID, Level: t.Level, YearsExperience: t.YearsExperience})
	}
	return resolved, unmapped, nil
}

func (r *postgresRepository) FindSkillMentionsInText(ctx context.Context, text string) ([]ResolvedSkill, error) {
	rows, err := r.db.Query(ctx, `
		select id, canonical_term as term from skills
		union
		select sk.id, syn.synonym_text as term from skill_synonyms syn join skills sk on sk.id = syn.skill_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lowerText := strings.ToLower(text)
	seen := make(map[string]bool)
	var resolved []ResolvedSkill
	for rows.Next() {
		var skillID, term string
		if err := rows.Scan(&skillID, &term); err != nil {
			return nil, err
		}
		if seen[skillID] || term == "" {
			continue
		}
		if mentionsTerm(text, lowerText, term) {
			resolved = append(resolved, ResolvedSkill{SkillID: skillID})
			seen[skillID] = true
		}
	}
	return resolved, rows.Err()
}

func (r *postgresRepository) InsertCandidateSkills(ctx context.Context, candidateID string, skills []ResolvedSkill) error {
	for _, s := range skills {
		if _, err := r.db.Exec(ctx, `
			insert into candidate_skills (candidate_id, skill_id, level, years_experience)
			values ($1, $2, nullif($3, ''), $4)
			on conflict (candidate_id, skill_id) do nothing
		`, candidateID, s.SkillID, s.Level, s.YearsExperience); err != nil {
			return err
		}
	}
	return nil
}

func (r *postgresRepository) InsertTalentSkills(ctx context.Context, talentID string, skills []ResolvedSkill) error {
	for _, s := range skills {
		if _, err := r.db.Exec(ctx, `
			insert into talent_skills (talent_id, skill_id, level, years_experience)
			values ($1, $2, nullif($3, ''), $4)
			on conflict (talent_id, skill_id) do nothing
		`, talentID, s.SkillID, s.Level, s.YearsExperience); err != nil {
			return err
		}
	}
	return nil
}

func (r *postgresRepository) QueueSkillReview(ctx context.Context, companyID, rawTerm string) error {
	// Um termo pendente por empresa (sem distinguir caixa): com IA extraindo skills de todo
	// currículo, o mesmo "Airflow" chegaria uma vez por candidato e a fila viraria ruído.
	_, err := r.db.Exec(ctx, `
		insert into skill_mapping_review_queue (company_id, raw_text)
		select $1::uuid, $2::text
		where not exists (
			select 1 from skill_mapping_review_queue
			where company_id = $1::uuid and status = 'pending' and lower(raw_text) = lower($2::text)
		)
	`, companyID, rawTerm)
	return err
}

func (r *postgresRepository) InsertTalentSectors(ctx context.Context, talentID string, names []string) error {
	for _, name := range names {
		if name == "" {
			continue
		}
		var sectorID string
		err := r.db.QueryRow(ctx, "select id from sectors where name = $1", name).Scan(&sectorID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue // sem fila de revisão pra setor — descarta o que não bate, ver comentário no Repository
			}
			return err
		}
		if _, err := r.db.Exec(ctx, `
			insert into talent_sectors (talent_id, sector_id) values ($1, $2) on conflict do nothing
		`, talentID, sectorID); err != nil {
			return err
		}
	}
	return nil
}

func (r *postgresRepository) InsertTalentLanguages(ctx context.Context, talentID string, langs []llm.LanguageMention) error {
	for _, l := range langs {
		if l.Name == "" {
			continue
		}
		var languageID string
		err := r.db.QueryRow(ctx, "select id from languages where name = $1", l.Name).Scan(&languageID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return err
		}
		if _, err := r.db.Exec(ctx, `
			insert into talent_languages (talent_id, language_id, proficiency) values ($1, $2, nullif($3, ''))
			on conflict do nothing
		`, talentID, languageID, l.Proficiency); err != nil {
			return err
		}
	}
	return nil
}

func (r *postgresRepository) InsertTalentSourceDocument(ctx context.Context, talentID, rawText string) error {
	_, err := r.db.Exec(ctx, `
		insert into talent_source_documents (talent_id, kind, raw_text) values ($1, 'resume', $2)
	`, talentID, rawText)
	return err
}
