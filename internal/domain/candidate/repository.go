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
	// LatestAssessmentID devolve o id da avaliação de IA mais recente do candidato NESTA FASE, ou ""
	// se ele ainda não foi avaliado nela — vira o par (recomendação × decisão) registrado em
	// candidate_decisions. Escopado por fase de propósito: antes buscava a mais recente entre TODAS
	// as fases, o que ligava a decisão a uma avaliação de uma etapa anterior quando o candidato
	// avançava sem ser reavaliado — corrigido junto da migration 0013.
	LatestAssessmentID(ctx context.Context, candidateID, phaseKey string) (string, error)
	// AssessmentHistory devolve TODA avaliação já gravada do candidato (uma por fase distinta —
	// distinct on phase_key, a mais recente quando há mais de uma versão de prompt na mesma fase),
	// mais antiga primeiro. É a trilha de "o que a IA dizia em cada etapa" que o schema já suporta
	// desde a v1 (ver comentário de candidate_ai_assessments em migrations/0001_init.sql) e que
	// nenhum caminho de leitura expunha até aqui.
	AssessmentHistory(ctx context.Context, candidateID string) ([]AIAssessmentHistoryEntry, error)
	// PriorStageAssessment devolve a avaliação mais recente do candidato numa fase DIFERENTE da
	// informada, ou nil, nil se esta é a primeira fase avaliada — usada pra dar à IA um resumo
	// curto do que já se sabia, em vez de reavaliar do zero (ver assessment.go, buildAssessInput).
	PriorStageAssessment(ctx context.Context, candidateID, excludePhaseKey string) (*AIAssessment, string, error)
	// AdvancePhase mata dois coelhos numa Exec só: move o candidato pra nextPhaseKey e já ajusta o
	// status pro que faz sentido na fase nova. Devolve false se o candidato não existe/não é desta
	// empresa (0 linhas afetadas).
	AdvancePhase(ctx context.Context, companyID, id, nextPhaseKey string, nextStatus Status) (bool, error)
	// Reject marca reprovado (status/motivo/rejected_at) e, se talentID != nil, já liga
	// candidates.talent_id na mesma escrita — nunca duas queries pra um estado que tem que nascer
	// junto.
	Reject(ctx context.Context, companyID, id, reasonKey string, talentID *string) (bool, error)
	CreateDecision(ctx context.Context, d *Decision) error
	// FindTalentIDByEmail acha a pessoa já registrada na empresa pelo e-mail (citext), ou "" — é a
	// deduplicação: a mesma pessoa não vira dois talentos por ter passado por dois caminhos.
	FindTalentIDByEmail(ctx context.Context, companyID, email string) (string, error)
	// CreateTalentFromCandidate registra a pessoa de um candidato que ainda não tinha registro de
	// talento (ex.: candidato inserido direto no banco), copiando TODO o perfil que a candidatura
	// tem — contato, resumo, formação, experiência e skills —, não só nome e e-mail. Nasce fora do
	// banco; quem o põe no banco é PromoteTalentToBank.
	CreateTalentFromCandidate(ctx context.Context, companyID, candidateID, origin string) (string, error)
	// PromoteTalentToBank põe o talento no Banco de Talentos (idempotente). requireConsent=true só
	// promove quem já consentiu (caminho da aprovação); false promove como "notificado" quem ainda
	// não tinha consentido (a reprovação envia o convite). Nunca promove quem pediu exclusão. Devolve
	// se o talento está no banco depois da chamada.
	PromoteTalentToBank(ctx context.Context, companyID, talentID, origin string, requireConsent bool) (bool, error)
	// LinkCandidateTalent liga a candidatura ao registro da pessoa (é o que monta o histórico dela no
	// banco). Só preenche quando ainda não havia vínculo.
	LinkCandidateTalent(ctx context.Context, companyID, candidateID, talentID string) error
	// CreateManualTalent grava o cadastro manual (fora de campanha), já dentro do banco.
	CreateManualTalent(ctx context.Context, t *TalentSeed) (string, error)

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
	// UpsertTalentFromPublicApplication grava o perfil rico (extraído pela IA ou preenchido
	// manualmente) — ou, se a pessoa já está registrada na empresa com este e-mail, ATUALIZA esse
	// registro com os dados novos em vez de criar um segundo. Não põe ninguém no banco: a candidatura
	// não é caminho de entrada (ver migrations/0012). Nunca referencia embedding.
	UpsertTalentFromPublicApplication(ctx context.Context, t *TalentSeed) (string, error)
	CreateCandidate(ctx context.Context, c *CandidateSeed) (string, error)
	InsertCandidateExperience(ctx context.Context, candidateID string, entries []ExperienceEntry) error
	// ReplaceTalentExperience troca a experiência do talento pela recebida: numa pessoa que já
	// existia, o currículo novo é a versão atual — acrescentar duplicaria as entradas.
	ReplaceTalentExperience(ctx context.Context, talentID string, entries []ExperienceEntry) error
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
			-- Escopado à fase ATUAL do candidato (c.phase_key, correlacionado) — antes pegava a
			-- avaliação mais recente entre TODAS as fases, então um candidato que avançava sem
			-- reavaliação mostrava o match de uma etapa anterior sob o rótulo da etapa nova.
			select match_pct from candidate_ai_assessments
			where candidate_id = c.id and phase_key = c.phase_key
			order by created_at desc limit 1
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

	ai, err := r.latestAssessment(ctx, id, d.PhaseKey)
	if err != nil {
		return nil, err
	}
	d.AI = ai

	history, err := r.AssessmentHistory(ctx, id)
	if err != nil {
		return nil, err
	}
	d.AIHistory = history

	return &d, nil
}

// latestAssessment busca a avaliação mais recente do candidato NESTA fase — não a mais recente
// entre todas (ver comentário em ListByCampaign pro porquê disso importar).
func (r *postgresRepository) latestAssessment(ctx context.Context, candidateID, phaseKey string) (*AIAssessment, error) {
	return r.queryAssessment(ctx, "where candidate_id = $1 and phase_key::text = $2 order by created_at desc limit 1",
		candidateID, phaseKey)
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
	var matchLabel, matchNote, justification, confidence, stageInsight, comparisonFlag *string
	row := r.db.QueryRow(ctx, `
		select id, match_pct, match_label, match_note, justification,
		       confidence, stage_insight, missing_information, comparison_flag
		from candidate_ai_assessments `+clause, args...)
	err := row.Scan(&assessmentID, &ai.MatchPct, &matchLabel, &matchNote, &justification,
		&confidence, &stageInsight, &ai.MissingInformation, &comparisonFlag)
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
	if confidence != nil {
		ai.Confidence = *confidence
	}
	if stageInsight != nil {
		ai.StageInsight = *stageInsight
	}
	if comparisonFlag != nil {
		ai.ComparisonFlag = *comparisonFlag
	}
	if ai.MissingInformation == nil {
		ai.MissingInformation = []string{}
	}

	strengths, concerns, err := r.loadAssessmentPoints(ctx, assessmentID)
	if err != nil {
		return nil, err
	}
	ai.Strengths, ai.Concerns = strengths, concerns
	return &ai, nil
}

// loadAssessmentPoints devolve pontos fortes e de atenção de uma avaliação — sempre slices
// non-nil, mesmo vazios (extraído de queryAssessment pra ser reaproveitado por AssessmentHistory e
// PriorStageAssessment sem duplicar a query de pontos 3 vezes).
func (r *postgresRepository) loadAssessmentPoints(ctx context.Context, assessmentID string) (strengths, concerns []string, err error) {
	rows, err := r.db.Query(ctx, `
		select kind, text from candidate_ai_assessment_points
		where assessment_id = $1 order by position asc
	`, assessmentID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	strengths, concerns = []string{}, []string{}
	for rows.Next() {
		var kind, text string
		if err := rows.Scan(&kind, &text); err != nil {
			return nil, nil, err
		}
		if kind == "strength" {
			strengths = append(strengths, text)
		} else {
			concerns = append(concerns, text)
		}
	}
	return strengths, concerns, rows.Err()
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
			(candidate_id, phase_key, match_pct, match_label, match_note, justification, provider, model,
			 prompt_version, confidence, stage_insight, missing_information, comparison_flag)
		values ($1, $2, $3, nullif($4, ''), nullif($5, ''), nullif($6, ''), $7, $8, $9,
			nullif($10, ''), nullif($11, ''), $12, nullif($13, ''))
		on conflict (candidate_id, phase_key, prompt_version) where prompt_version is not null do nothing
		returning id
	`, candidateID, phaseKey, a.MatchPct, a.MatchLabel, a.MatchNote, a.Justification,
		origin.Provider, origin.Model, origin.PromptVersion,
		a.Confidence, a.StageInsight, a.MissingInformation, a.ComparisonFlag).Scan(&assessmentID)
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

// LatestAssessmentID é escopado à fase informada — a avaliação "que estava na tela" quando o RH
// decide é sempre a da fase em que o candidato está decidindo, nunca a mais recente entre todas
// (mesmo motivo do comentário em ListByCampaign).
func (r *postgresRepository) LatestAssessmentID(ctx context.Context, candidateID, phaseKey string) (string, error) {
	var id string
	row := r.db.QueryRow(ctx, `
		select id from candidate_ai_assessments
		where candidate_id = $1 and phase_key::text = $2
		order by created_at desc limit 1
	`, candidateID, phaseKey)
	err := row.Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return id, nil
}

// AssessmentHistory devolve a avaliação mais recente de cada fase já avaliada deste candidato
// (uma por fase — distinct on phase_key, a de created_at mais recente quando há mais de uma versão
// de prompt na mesma fase), na ordem do FUNIL DESTA CAMPANHA — não a ordem de declaração do enum
// phase_key, que não reflete a reordenação de fases opcionais que a campanha pode ter (ver
// campaign_phases.position). recebidos/selecionados são as bordas fixas do funil (não têm linha em
// campaign_phases, só as 3 fases opcionais têm) — por isso o coalesce nos extremos.
func (r *postgresRepository) AssessmentHistory(ctx context.Context, candidateID string) ([]AIAssessmentHistoryEntry, error) {
	rows, err := r.db.Query(ctx, `
		select id, phase_key, match_pct, match_label, match_note, justification,
		       confidence, stage_insight, missing_information, comparison_flag, created_at
		from (
			select distinct on (a.phase_key)
			       a.id, a.phase_key::text as phase_key, a.match_pct, a.match_label, a.match_note,
			       a.justification, a.confidence, a.stage_insight, a.missing_information,
			       a.comparison_flag, a.created_at,
			       coalesce(cp.position,
			         case a.phase_key when 'recebidos' then -1 when 'selecionados' then 999 else 0 end
			       ) as funnel_position
			from candidate_ai_assessments a
			join candidates c on c.id = a.candidate_id
			left join campaign_phases cp on cp.campaign_id = c.campaign_id and cp.phase_key = a.phase_key
			where a.candidate_id = $1
			order by a.phase_key, a.created_at desc
		) latest_per_phase
		order by funnel_position asc
	`, candidateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]AIAssessmentHistoryEntry, 0)
	for rows.Next() {
		var e AIAssessmentHistoryEntry
		var assessmentID string
		var matchLabel, matchNote, justification, confidence, stageInsight, comparisonFlag *string
		if err := rows.Scan(&assessmentID, &e.PhaseKey, &e.Assessment.MatchPct, &matchLabel, &matchNote,
			&justification, &confidence, &stageInsight, &e.Assessment.MissingInformation, &comparisonFlag,
			&e.CreatedAt); err != nil {
			return nil, err
		}
		if matchLabel != nil {
			e.Assessment.MatchLabel = *matchLabel
		}
		if matchNote != nil {
			e.Assessment.MatchNote = *matchNote
		}
		if justification != nil {
			e.Assessment.Justification = *justification
		}
		if confidence != nil {
			e.Assessment.Confidence = *confidence
		}
		if stageInsight != nil {
			e.Assessment.StageInsight = *stageInsight
		}
		if comparisonFlag != nil {
			e.Assessment.ComparisonFlag = *comparisonFlag
		}
		if e.Assessment.MissingInformation == nil {
			e.Assessment.MissingInformation = []string{}
		}
		strengths, concerns, err := r.loadAssessmentPoints(ctx, assessmentID)
		if err != nil {
			return nil, err
		}
		e.Assessment.Strengths, e.Assessment.Concerns = strengths, concerns
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// PriorStageAssessment devolve a avaliação mais recente do candidato numa fase diferente da
// informada — na prática, quase sempre a da fase imediatamente anterior, porque a avaliação de uma
// fase só é pedida depois de o candidato já ter avançado até ela. "" quando não há nenhuma.
func (r *postgresRepository) PriorStageAssessment(ctx context.Context, candidateID, excludePhaseKey string) (*AIAssessment, string, error) {
	var assessmentID, phaseKey string
	var ai AIAssessment
	var matchLabel, matchNote, justification, confidence, stageInsight, comparisonFlag *string
	row := r.db.QueryRow(ctx, `
		select id, phase_key::text, match_pct, match_label, match_note, justification,
		       confidence, stage_insight, missing_information, comparison_flag
		from candidate_ai_assessments
		where candidate_id = $1 and phase_key::text <> $2
		order by created_at desc limit 1
	`, candidateID, excludePhaseKey)
	err := row.Scan(&assessmentID, &phaseKey, &ai.MatchPct, &matchLabel, &matchNote, &justification,
		&confidence, &stageInsight, &ai.MissingInformation, &comparisonFlag)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil
		}
		return nil, "", err
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
	if confidence != nil {
		ai.Confidence = *confidence
	}
	if stageInsight != nil {
		ai.StageInsight = *stageInsight
	}
	if comparisonFlag != nil {
		ai.ComparisonFlag = *comparisonFlag
	}
	if ai.MissingInformation == nil {
		ai.MissingInformation = []string{}
	}
	strengths, concerns, err := r.loadAssessmentPoints(ctx, assessmentID)
	if err != nil {
		return nil, "", err
	}
	ai.Strengths, ai.Concerns = strengths, concerns
	return &ai, phaseKey, nil
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

func (r *postgresRepository) FindTalentIDByEmail(ctx context.Context, companyID, email string) (string, error) {
	if email == "" {
		return "", nil
	}
	var id string
	err := r.db.QueryRow(ctx, `
		select id from talents where company_id = $1 and email = $2 and deleted_at is null
	`, companyID, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (r *postgresRepository) CreateTalentFromCandidate(ctx context.Context, companyID, candidateID, origin string) (string, error) {
	var id string
	err := r.db.QueryRow(ctx, `
		insert into talents (company_id, name, email, phone, linkedin_url, city, state, years_experience, summary,
		                     education_degree, education_institution, education_period,
		                     origin, legal_basis, consent_state)
		select company_id, name, email, phone, linkedin_url, city, state, years_experience, summary,
		       education_degree, education_institution, education_period,
		       $3::talent_origin, 'legitimo_interesse', 'nao_notificado'
		from candidates where id = $1 and company_id = $2 and deleted_at is null
		returning id
	`, candidateID, companyID, origin).Scan(&id)
	if err != nil {
		return "", err
	}
	if _, err := r.db.Exec(ctx, `
		insert into talent_experience_entries (talent_id, role, company, period_label, description, position)
		select $1, role, company, period_label, description, position
		from candidate_experience_entries where candidate_id = $2
	`, id, candidateID); err != nil {
		return "", err
	}
	if _, err := r.db.Exec(ctx, `
		insert into talent_skills (talent_id, skill_id, level, years_experience)
		select $1, skill_id, level, years_experience from candidate_skills where candidate_id = $2
		on conflict (talent_id, skill_id) do nothing
	`, id, candidateID); err != nil {
		return "", err
	}
	return id, nil
}

func (r *postgresRepository) PromoteTalentToBank(ctx context.Context, companyID, talentID, origin string, requireConsent bool) (bool, error) {
	// Quem já consentiu fica como está (consentimento é o estado mais forte); quem não consentiu
	// passa a "notificado", porque a reprovação é justamente o momento em que o convite é enviado.
	// A origem só é gravada na PRIMEIRA entrada: ela diz por onde a pessoa entrou no banco.
	tag, err := r.db.Exec(ctx, `
		update talents set
			origin          = case when bank_entered_at is null then $3::talent_origin else origin end,
			bank_entered_at = coalesce(bank_entered_at, now()),
			legal_basis     = case when consent_state = 'consentido' then legal_basis else 'legitimo_interesse' end,
			consent_state   = case when consent_state = 'consentido' then consent_state else 'notificado' end
		where id = $1 and company_id = $2 and deleted_at is null
		  and consent_state <> 'oposicao_exclusao'
		  and (not $4 or consent_state = 'consentido')
	`, talentID, companyID, origin, requireConsent)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *postgresRepository) LinkCandidateTalent(ctx context.Context, companyID, candidateID, talentID string) error {
	_, err := r.db.Exec(ctx, `
		update candidates set talent_id = $3
		where id = $1 and company_id = $2 and talent_id is null
	`, candidateID, companyID, talentID)
	return err
}

func (r *postgresRepository) CreateManualTalent(ctx context.Context, t *TalentSeed) (string, error) {
	var id string
	err := r.db.QueryRow(ctx, `
		insert into talents (company_id, name, email, linkedin_url, city, state, modality, seniority,
		                     years_experience, summary, recruiter_notes,
		                     education_degree, education_institution, education_period,
		                     origin, legal_basis, consent_state, bank_entered_at)
		values ($1, $2, nullif($3, ''), nullif($4, ''), nullif($5, ''), nullif($6, ''), nullif($7, ''), nullif($8, ''),
		        $9, nullif($10, ''), nullif($11, ''), nullif($12, ''), nullif($13, ''), nullif($14, ''),
		        'cadastro_manual', 'legitimo_interesse', 'nao_notificado', now())
		returning id
	`, t.CompanyID, t.Name, t.Email, t.LinkedInURL, t.City, t.State, t.Modality, t.Seniority,
		t.YearsExperience, t.Summary, t.RecruiterNotes,
		t.EducationDegree, t.EducationInstitution, t.EducationPeriod).Scan(&id)
	if err != nil {
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

func (r *postgresRepository) UpsertTalentFromPublicApplication(ctx context.Context, t *TalentSeed) (string, error) {
	// ON CONFLICT no índice único (empresa, e-mail) de migrations/0012. Na atualização: dado novo
	// preenchido vence, vazio não apaga o que existia; a origem e a entrada no banco não mudam; e o
	// consentimento é renovado — a pessoa acabou de consentir de novo, neste formulário.
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
		on conflict (company_id, email) where deleted_at is null and email is not null do update set
			name                  = excluded.name,
			phone                 = coalesce(excluded.phone, talents.phone),
			city                  = coalesce(excluded.city, talents.city),
			state                 = coalesce(excluded.state, talents.state),
			linkedin_url          = coalesce(excluded.linkedin_url, talents.linkedin_url),
			modality              = coalesce(excluded.modality, talents.modality),
			seniority             = coalesce(excluded.seniority, talents.seniority),
			years_experience      = coalesce(excluded.years_experience, talents.years_experience),
			salary_min            = coalesce(excluded.salary_min, talents.salary_min),
			salary_max            = coalesce(excluded.salary_max, talents.salary_max),
			available_from        = coalesce(excluded.available_from, talents.available_from),
			availability_note     = coalesce(excluded.availability_note, talents.availability_note),
			summary               = coalesce(excluded.summary, talents.summary),
			education_degree      = coalesce(excluded.education_degree, talents.education_degree),
			education_institution = coalesce(excluded.education_institution, talents.education_institution),
			education_period      = coalesce(excluded.education_period, talents.education_period),
			legal_basis           = excluded.legal_basis,
			consent_state         = excluded.consent_state,
			consent_date          = excluded.consent_date,
			profile_reviewed_at   = now()
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

func (r *postgresRepository) ReplaceTalentExperience(ctx context.Context, talentID string, entries []ExperienceEntry) error {
	if _, err := r.db.Exec(ctx, `delete from talent_experience_entries where talent_id = $1`, talentID); err != nil {
		return err
	}
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
