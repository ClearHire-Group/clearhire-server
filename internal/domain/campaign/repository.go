package campaign

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
)

type Repository interface {
	FindByID(ctx context.Context, companyID, id string) (*Campaign, error)
	ListByCompany(ctx context.Context, companyID string) ([]Campaign, error)
	Create(ctx context.Context, c *Campaign, phases []Phase) error
	// CreateCandidatesFromTalents cria um candidato inicial (fase Recebidos) para cada talento do
	// banco escolhido — no match reverso do assistente de criação (dentro da MESMA transação que
	// Create) ou depois, numa campanha já existente (ver AddTalentsToCampaign). Devolve os
	// talentIDs que efetivamente viraram candidato: um id que não existe, não é desta empresa,
	// que já é candidato desta campanha (colisão de e-mail), ou cujo consent_state não autoriza
	// uso ativo em processo seletivo (ver ELIGIBLE_CONSENT_STATES logo abaixo) fica de fora da
	// lista devolvida, sem abortar os demais — a seleção veio de uma tela que pode estar um passo
	// desatualizada, e a resposta certa a UM id problemático nunca é derrubar o lote inteiro.
	CreateCandidatesFromTalents(ctx context.Context, companyID, campaignID string, talentIDs []string) ([]string, error)
	// FindTalentsEligibility devolve nome + consent_state dos ids pedidos — usado só pra explicar
	// ao recrutador por que um talento não entrou (ver TalentEligibility).
	FindTalentsEligibility(ctx context.Context, companyID string, talentIDs []string) ([]TalentEligibility, error)
	SetStatus(ctx context.Context, companyID, id string, status Status, pausedAt *time.Time) error
	// SetPublicApplicationsEnabled liga/desliga o link de candidatura pública — estado desejado
	// explícito (não um toggle cego), porque "gerar link" e "desativar link" são duas ações
	// distintas e idempotentes do recrutador, não uma alternância simétrica.
	SetPublicApplicationsEnabled(ctx context.Context, companyID, id string, enabled bool) error
	// UpdateDetails atualiza os campos editáveis da tela "Configurações da Campanha" — nunca
	// status/timestamps/fases, que têm seus próprios métodos dedicados.
	UpdateDetails(ctx context.Context, companyID, id string, in UpdateDetailsInput) error
	// CandidateCountsByPhase é a contagem EXATA (não cumulativa, diferente de PhaseCountsByCampaign)
	// de candidatos por phase_key — usada só pra decidir se uma fase pode ser removida do funil.
	CandidateCountsByPhase(ctx context.Context, companyID, campaignID string) (map[string]int, error)
	// ReplacePhases substitui as fases OPCIONAIS de uma campanha (Recebidos/Selecionados sempre
	// presentes na lista completa dada) — chamado só depois que o service já validou que nenhuma
	// fase removida tem candidato nela. phases já vem na ordem final completa (ver buildPhases).
	ReplacePhases(ctx context.Context, campaignID string, phases []Phase) error
	// FindPublicByID NÃO recebe companyID — quem chama é anônimo, sem tenant nenhum. A própria
	// query faz a checagem de elegibilidade (status ativa + link ligado + não deletada); nil, nil
	// cobre "não existe" e "existe mas não está elegível" com o mesmo resultado, de propósito —
	// nunca dar pista de qual dos dois é o caso pra quem chama de fora.
	FindPublicByID(ctx context.Context, id string) (*PublicInfo, error)
	// PhaseCountsByCampaign/PhaseCountsByCompany devolvem a contagem CUMULATIVA por fase —
	// candidatos cuja fase atual está nesta posição do funil ou adiante, nunca uma contagem
	// exata "quem está sentado aqui agora". Ver PhaseCount no model.go pro porquê.
	PhaseCountsByCampaign(ctx context.Context, companyID, campaignID string) ([]PhaseCount, error)
	// PhaseCountsByCompany recorta por período de CANDIDATURA (ver ReportPeriod); o zero value
	// não recorta nada, que é o que a tela de Campanhas usa.
	PhaseCountsByCompany(ctx context.Context, companyID string, period ReportPeriod) ([]PhaseCount, error)
	// HiredCount só é relevante pra campanhas encerradas (usado no texto de `meta`) — ninguém
	// ainda produz esse estado nesta versão, então é sempre chamado sob demanda, não em massa.
	HiredCount(ctx context.Context, companyID, campaignID string) (int, error)
}

type postgresRepository struct {
	db database.DB
}

func NewRepository(db database.DB) Repository {
	return &postgresRepository{db: db}
}

const campaignColumns = `id, company_id, created_by_user_id, title, coalesce(description, ''),
	coalesce(responsibilities, ''), coalesce(requirements, ''), coalesce(benefits, ''),
	coalesce(city, ''), coalesce(state, ''),
	modality, contract_type, seniority, status, accepts_public_applications, opened_at, paused_at, closed_at, created_at, updated_at`

func scanCampaign(row pgx.Row) (*Campaign, error) {
	var c Campaign
	err := row.Scan(
		&c.ID, &c.CompanyID, &c.CreatedByUserID, &c.Title, &c.Description,
		&c.Responsibilities, &c.Requirements, &c.Benefits,
		&c.City, &c.State,
		&c.Modality, &c.ContractType, &c.Seniority, &c.Status, &c.AcceptsPublicApplications, &c.OpenedAt, &c.PausedAt, &c.ClosedAt,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *postgresRepository) FindByID(ctx context.Context, companyID, id string) (*Campaign, error) {
	row := r.db.QueryRow(ctx, `
		select `+campaignColumns+`
		from campaigns where id = $1 and company_id = $2 and deleted_at is null
	`, id, companyID)
	return scanCampaign(row)
}

func (r *postgresRepository) ListByCompany(ctx context.Context, companyID string) ([]Campaign, error) {
	rows, err := r.db.Query(ctx, `
		select `+campaignColumns+`
		from campaigns where company_id = $1 and deleted_at is null
		order by created_at desc
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var campaigns []Campaign
	for rows.Next() {
		c, err := scanCampaign(rows)
		if err != nil {
			return nil, err
		}
		campaigns = append(campaigns, *c)
	}
	return campaigns, rows.Err()
}

func (r *postgresRepository) Create(ctx context.Context, c *Campaign, phases []Phase) error {
	row := r.db.QueryRow(ctx, `
		insert into campaigns (company_id, created_by_user_id, title, description, responsibilities, requirements,
		                       benefits, city, state, modality, contract_type, seniority)
		values ($1, $2, $3, nullif($4, ''), nullif($5, ''), nullif($6, ''),
		        nullif($7, ''), nullif($8, ''), nullif($9, ''), $10, $11, $12)
		returning id, status, opened_at, created_at, updated_at
	`, c.CompanyID, c.CreatedByUserID, c.Title, c.Description, c.Responsibilities, c.Requirements,
		c.Benefits, c.City, c.State, c.Modality, c.ContractType, c.Seniority)
	if err := row.Scan(&c.ID, &c.Status, &c.OpenedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return err
	}

	for _, p := range phases {
		if _, err := r.db.Exec(ctx, `
			insert into campaign_phases (campaign_id, phase_key, position)
			values ($1, $2, $3)
		`, c.ID, p.Key, p.Position); err != nil {
			return err
		}
	}
	return nil
}

// eligibleConsentStatesSQL é o filtro de LGPD de quem pode virar candidato ATIVO de uma campanha
// (diferente do filtro mais frouxo de leitura/match, que só bloqueia oposicao_exclusao — ver
// ReverseMatch em talent/reversematch.go). Puxar alguém pro funil não é só "ler o perfil": é o
// primeiro passo de um processo seletivo de verdade, que pode terminar em contato, avaliação e
// decisão registrados em nome da pessoa.
//
//   - 'consentido': a pessoa já autorizou explicitamente o reaproveitamento em vaga futura
//     (documento de especificação do Banco de Talentos, seção 5.1) — caso direto.
//   - 'notificado': foi informada do tratamento e não se opôs (seção 5.2) — base legal de
//     legítimo interesse já comunicado, suficiente pra este uso.
//   - 'nao_notificado' FICA DE FORA de propósito: a pessoa ainda nem sabe que está no banco
//     (comum em cadastro manual — "achei no LinkedIn"). Torná-la candidata ativa de uma vaga sem
//     que o aviso de tratamento tenha disparado pula exatamente a etapa que o documento describe
//     como o gatilho certo (seção 5.2: "quando o recrutador faz o primeiro contato real... o
//     sistema dispara o aviso de tratamento"). O caminho correto é MarkFirstContact primeiro
//     (POST /talents/:id/first-contact, já existe) — só then a pessoa fica elegível aqui.
//   - 'oposicao_exclusao' nunca entra, sem exceção.
const eligibleConsentStatesSQL = `t.consent_state in ('consentido', 'notificado')`

func (r *postgresRepository) CreateCandidatesFromTalents(ctx context.Context, companyID, campaignID string, talentIDs []string) ([]string, error) {
	if len(talentIDs) == 0 {
		return nil, nil
	}
	// Duas CTEs de escrita encadeadas (candidato, depois skills copiadas) mais um SELECT final que
	// só lê de `created` — "criar o candidato" e "copiar as skills dele" continuam sendo uma
	// unidade atômica (sem isso, uma falha entre os dois deixaria candidato sem skill nenhuma,
	// quebrando a mesma maquinária de avaliação que já espera candidate_skills preenchido — ver
	// candidate/skillmatch.go), e o SELECT é o que devolve pro chamador quem de fato entrou.
	//
	// ON CONFLICT no e-mail: mesmo índice parcial de migrations/0010 (uq_candidates_campaign_email).
	// Sem isto, pedir pra adicionar um talento que por acaso já é candidato desta campanha (e-mail
	// repetido) derrubaria a transação INTEIRA com violação de unicidade — os outros talentos do
	// mesmo pedido, que não tinham nada de errado, seriam recusados junto por causa de UM só.
	rows, err := r.db.Query(ctx, `
		with created as (
			insert into candidates (company_id, campaign_id, talent_id, name, email, phone, linkedin_url,
			                        city, state, years_experience, phase_key, status, summary,
			                        education_degree, education_institution, education_period)
			select $2, $3, t.id, t.name, t.email, t.phone, t.linkedin_url, t.city, t.state,
			       t.years_experience, 'recebidos', 'aguardando_triagem_ia', t.summary,
			       t.education_degree, t.education_institution, t.education_period
			from talents t
			where t.id = any($1) and t.company_id = $2
			  and `+eligibleConsentStatesSQL+` and t.bank_entered_at is not null
			on conflict (campaign_id, email) where deleted_at is null and email is not null do nothing
			returning id, talent_id
		), skills_copied as (
			insert into candidate_skills (candidate_id, skill_id, level, years_experience)
			select created.id, ts.skill_id, ts.level, ts.years_experience
			from created join talent_skills ts on ts.talent_id = created.talent_id
			returning 1
		)
		select talent_id from created
	`, talentIDs, companyID, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var added []string
	for rows.Next() {
		var talentID string
		if err := rows.Scan(&talentID); err != nil {
			return nil, err
		}
		added = append(added, talentID)
	}
	return added, rows.Err()
}

// TalentEligibility é o que AddTalentsToCampaign usa pra explicar, id a id, por que um talento
// pedido não entrou como candidato — nunca um silêncio sobre uma seleção que o recrutador fez.
type TalentEligibility struct {
	ID, Name, ConsentState string
}

// FindTalentsEligibility lê nome + consent_state dos ids pedidos (só isso, é o suficiente pra
// montar a mensagem de "por que não" quando CreateCandidatesFromTalents deixa alguém de fora).
// Ids que não existem nesta empresa simplesmente não aparecem no retorno.
func (r *postgresRepository) FindTalentsEligibility(ctx context.Context, companyID string, talentIDs []string) ([]TalentEligibility, error) {
	rows, err := r.db.Query(ctx, `
		select id, name, consent_state::text from talents
		where id = any($1) and company_id = $2 and bank_entered_at is not null
	`, talentIDs, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TalentEligibility{}
	for rows.Next() {
		var e TalentEligibility
		if err := rows.Scan(&e.ID, &e.Name, &e.ConsentState); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *postgresRepository) SetStatus(ctx context.Context, companyID, id string, status Status, pausedAt *time.Time) error {
	_, err := r.db.Exec(ctx, `
		update campaigns set status = $3, paused_at = $4
		where id = $1 and company_id = $2 and deleted_at is null
	`, id, companyID, status, pausedAt)
	return err
}

func (r *postgresRepository) SetPublicApplicationsEnabled(ctx context.Context, companyID, id string, enabled bool) error {
	_, err := r.db.Exec(ctx, `
		update campaigns set accepts_public_applications = $3
		where id = $1 and company_id = $2 and deleted_at is null
	`, id, companyID, enabled)
	return err
}

// UpdateDetailsInput é o subconjunto de Campaign editável pela tela "Configurações da Campanha" —
// nunca status/timestamps/fases, que têm seus próprios métodos dedicados.
type UpdateDetailsInput struct {
	Title            string
	Description      string
	Responsibilities string
	Requirements     string
	Benefits         string
	City             string
	State            string
	Modality         string
	ContractType     string
	Seniority        string
}

func (r *postgresRepository) UpdateDetails(ctx context.Context, companyID, id string, in UpdateDetailsInput) error {
	_, err := r.db.Exec(ctx, `
		update campaigns
		set title = $3, description = nullif($4, ''), responsibilities = nullif($5, ''),
		    requirements = nullif($6, ''), benefits = nullif($7, ''),
		    city = nullif($8, ''), state = nullif($9, ''),
		    modality = $10, contract_type = $11, seniority = $12
		where id = $1 and company_id = $2 and deleted_at is null
	`, id, companyID, in.Title, in.Description, in.Responsibilities, in.Requirements, in.Benefits,
		in.City, in.State, in.Modality, in.ContractType, in.Seniority)
	return err
}

// CandidateCountsByPhase devolve a contagem EXATA de candidatos por phase_key (quem está
// sentado ali agora, não cumulativa como PhaseCountsByCampaign) — só serve pra decidir se uma
// fase pode ser removida do funil sem violar a FK (campaign_id, phase_key) de candidates.
func (r *postgresRepository) CandidateCountsByPhase(ctx context.Context, companyID, campaignID string) (map[string]int, error) {
	rows, err := r.db.Query(ctx, `
		select c.phase_key, count(*)
		from candidates c
		join campaigns camp on camp.id = c.campaign_id and camp.company_id = $1 and camp.deleted_at is null
		where c.campaign_id = $2 and c.deleted_at is null
		group by c.phase_key
	`, companyID, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		counts[key] = count
	}
	return counts, rows.Err()
}

// ReplacePhases substitui as fases de uma campanha pela lista final dada (já com Recebidos/
// Selecionados incluídos — ver buildPhases). O UPSERT + o DELETE na mesma transação dependem da
// constraint unique(campaign_id, position) ser DEFERRABLE (ver migrations/0001_init.sql) pra
// trocas de posição não colidirem no meio do caminho.
func (r *postgresRepository) ReplacePhases(ctx context.Context, campaignID string, phases []Phase) error {
	keys := make([]string, len(phases))
	for i, p := range phases {
		keys[i] = p.Key
	}
	if _, err := r.db.Exec(ctx, `
		delete from campaign_phases where campaign_id = $1 and phase_key <> all($2)
	`, campaignID, keys); err != nil {
		return err
	}

	for _, p := range phases {
		if _, err := r.db.Exec(ctx, `
			insert into campaign_phases (campaign_id, phase_key, position)
			values ($1, $2, $3)
			on conflict (campaign_id, phase_key) do update set position = excluded.position
		`, campaignID, p.Key, p.Position); err != nil {
			return err
		}
	}
	return nil
}

func (r *postgresRepository) FindPublicByID(ctx context.Context, id string) (*PublicInfo, error) {
	row := r.db.QueryRow(ctx, `
		select c.id, c.title, co.name, coalesce(c.description, ''), coalesce(c.responsibilities, ''),
		       coalesce(c.requirements, ''), coalesce(c.benefits, ''), coalesce(c.city, ''), coalesce(c.state, ''),
		       c.modality, c.contract_type, c.seniority
		from campaigns c
		join companies co on co.id = c.company_id
		where c.id = $1 and c.status = 'ativa' and c.accepts_public_applications and c.deleted_at is null
	`, id)
	var info PublicInfo
	err := row.Scan(&info.ID, &info.Title, &info.CompanyName, &info.Description, &info.Responsibilities,
		&info.Requirements, &info.Benefits, &info.City, &info.State,
		&info.Modality, &info.ContractType, &info.Seniority)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &info, nil
}

// PhaseCountsByCampaign/PhaseCountsByCompany compartilham a mesma forma de query: um self-join
// de campaign_phases contra ela mesma em `position >= `, seguido de left join com candidates (o
// left join é o que garante que uma fase sem candidato nenhum aparece como 0 em vez de sumir da
// lista — essencial enquanto a tabela candidates estiver vazia, como é hoje). A comparação por
// POSIÇÃO configurada é obrigatória, não a ordem do enum phase_key: a tela Nova Campanha deixa
// reordenar os módulos opcionais (fit/tecnica/entrevista) em qualquer ordem por campanha.
const phaseCountsQuery = `
	select target.campaign_id, target.phase_key, target.position, count(distinct c.id)
	from campaign_phases target
	join campaigns camp
	  on camp.id = target.campaign_id and camp.company_id = $1 and camp.deleted_at is null
	join campaign_phases own_cp
	  on own_cp.campaign_id = target.campaign_id and own_cp.position >= target.position
	left join candidates c
	  on c.campaign_id = own_cp.campaign_id and c.phase_key = own_cp.phase_key and c.deleted_at is null`

func (r *postgresRepository) queryPhaseCounts(ctx context.Context, sql string, args ...any) ([]PhaseCount, error) {
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var counts []PhaseCount
	for rows.Next() {
		var pc PhaseCount
		if err := rows.Scan(&pc.CampaignID, &pc.Key, &pc.Position, &pc.Count); err != nil {
			return nil, err
		}
		counts = append(counts, pc)
	}
	return counts, rows.Err()
}

func (r *postgresRepository) PhaseCountsByCampaign(ctx context.Context, companyID, campaignID string) ([]PhaseCount, error) {
	return r.queryPhaseCounts(ctx, phaseCountsQuery+`
		where target.campaign_id = $2
		group by target.campaign_id, target.phase_key, target.position
		order by target.position
	`, companyID, campaignID)
}

func (r *postgresRepository) PhaseCountsByCompany(ctx context.Context, companyID string, period ReportPeriod) ([]PhaseCount, error) {
	args := []any{companyID}
	// O recorte entra no ON do left join, NÃO num where: no where, uma campanha sem nenhuma
	// candidatura no período sairia da lista inteira (o left join produz linha com c.* nulo, que
	// o where descartaria). No ON, ela continua aparecendo com contagem zero — que é a resposta
	// certa pra "esta campanha não recebeu ninguém neste mês".
	filter := ""
	if period.From != nil {
		args = append(args, *period.From)
		filter += fmt.Sprintf(" and c.created_at >= $%d", len(args))
	}
	if period.To != nil {
		args = append(args, *period.To)
		filter += fmt.Sprintf(" and c.created_at < $%d", len(args))
	}

	return r.queryPhaseCounts(ctx, phaseCountsQuery+filter+`
		group by target.campaign_id, target.phase_key, target.position
		order by target.campaign_id, target.position
	`, args...)
}

func (r *postgresRepository) HiredCount(ctx context.Context, companyID, campaignID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		select count(*) from candidates
		where campaign_id = $1 and company_id = $2 and status = 'contratado' and deleted_at is null
	`, campaignID, companyID).Scan(&count)
	return count, err
}
