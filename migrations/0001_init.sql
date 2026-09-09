-- ============================================================================
-- Clearhire — modelagem de banco de dados base (PostgreSQL)
-- ============================================================================
-- Ponto de partida para o backend. Traduz pra SQL o que já existe no protótipo
-- Angular (src/app/core/models.ts) + as regras do documento de especificação
-- do Banco de Talentos (documentos/requisitos/Clearhire-banco-de-talentos-
-- especificacao.docx) + a regra de negócio nova: uma empresa cliente tem até
-- N contas de RH (N = 2 hoje, configurável por empresa).
--
-- Convenções adotadas em todo o arquivo:
--   - PK sempre uuid (gen_random_uuid()) — nunca a string "amigável" (slug/kebab-
--     case) que o protótipo usa como id hoje. Motivo: essas strings colidem
--     entre empresas clientes diferentes ("engenheiro-de-software" existiria em
--     N empresas) e a plataforma é multi-tenant desde o primeiro RH cadastrado.
--   - created_at / updated_at em toda tabela "viva", com trigger de verdade
--     mantendo o updated_at (`default now()` só roda no INSERT).
--   - deleted_at (soft delete) nas entidades que um recrutador pode querer
--     desfazer ou auditar depois — dado de RH é sensível, apagar de verdade
--     fecha a porta pra correção de erro e pra auditoria de decisão.
--   - Nada de score/match armazenado como atributo do talento — invariante do
--     documento de especificação (seções 6.3 e 11).
--   - Métricas agregadas (dashboard, relatórios, cobertura) não são tabelas —
--     são queries. Ver o bloco "MÉTRICAS COMPUTADAS" ao final do arquivo.
--   - Isolamento entre empresas é garantido pelo BANCO, não pela boa memória de
--     quem escreve o endpoint: toda tabela que pode cruzar tenants carrega
--     `company_id` e usa chave estrangeira COMPOSTA. Ver o bloco logo abaixo.
--   - A ordem das seções segue dependência de FK, não a ordem narrativa do
--     produto — por isso Talentos vem antes de Campanhas/Candidatos.
--
-- ISOLAMENTO ENTRE EMPRESAS (padrão aplicado em todo o arquivo)
--   Cada tabela-pai que participa de uma relação cruzada declara um
--   `unique (id, company_id)` — redundante como dado, necessário como alvo de
--   FK composta. Os filhos então referenciam O PAR, não só o id:
--
--       foreign key (talent_id, company_id) references talents (id, company_id)
--
--   Assim o banco recusa fisicamente um candidato da Empresa A apontando pra
--   um talento da Empresa B. Com colunas nuláveis o Postgres usa MATCH SIMPLE:
--   se `talent_id` é NULL a checagem é ignorada, que é exatamente o desejado
--   pra um vínculo opcional.
-- ============================================================================

create extension if not exists pgcrypto;   -- gen_random_uuid()
create extension if not exists citext;     -- e-mail e taxonomia case-insensitive
create extension if not exists vector;     -- pgvector, busca semântica (seção 6.2 do doc)


-- ============================================================================
-- 0. FUNÇÕES DE APOIO
-- ============================================================================

-- `updated_at timestamptz default now()` só preenche no INSERT — o Postgres não
-- atualiza sozinho depois. Sem este trigger a coluna congela na criação.
create or replace function touch_updated_at() returns trigger as $$
begin
  new.updated_at := now();
  return new;
end;
$$ language plpgsql;


-- ============================================================================
-- 1. TENANT, USUÁRIOS E AUTENTICAÇÃO
-- ============================================================================
-- Uma empresa contrata o Clearhire e tem direito a `seat_limit` contas de RH
-- ATIVAS (hoje 2). O limite fica na empresa, não hardcoded em código, porque é
-- claramente uma regra de plano/pricing que vai variar — não uma lei fixa.

create table companies (
  id                      uuid primary key default gen_random_uuid(),
  name                    text not null,
  -- Perfil cultural (era `CompanyProfile` no front) — herdado por toda campanha nova.
  culture_tone            text,
  culture_importance_note text,
  seat_limit              int not null default 2 check (seat_limit > 0),
  created_at              timestamptz not null default now(),
  updated_at              timestamptz not null default now(),
  -- Encerramento de contrato é soft delete: o cliente sai, o dado continua sob
  -- política de retenção. O DELETE de verdade existe e cascateia por tudo —
  -- ele é o caminho de exclusão definitiva a pedido, não o de offboarding.
  deleted_at              timestamptz
);

create trigger trg_companies_touch before update on companies
  for each row execute function touch_updated_at();

-- Valores organizacionais (chips na tela de Configurações). Tabela filha em vez
-- de um array/coluna porque cada valor precisa de posição estável na UI.
create table company_culture_values (
  id          uuid primary key default gen_random_uuid(),
  company_id  uuid not null references companies(id) on delete cascade,
  value       text not null,
  position    int not null default 0,
  unique (company_id, value)
);

create type user_role as enum ('owner', 'member');

-- Uma conta de RH. `role`: 'owner' é quem contratou/criou a empresa e pode
-- convidar ou remover o outro assento; 'member' é o segundo RH convidado.
--
-- DECISÃO REGISTRADA: `email` é único GLOBALMENTE, não por empresa. Consequência
-- assumida: a mesma pessoa não pode ser RH de duas empresas clientes com o mesmo
-- e-mail. O contrário (unique por company_id) exigiria escolher a empresa no
-- login. Se um dia isso virar caso real, é aqui que muda.
create table users (
  id             uuid primary key default gen_random_uuid(),
  company_id     uuid not null references companies(id) on delete cascade,
  name           text not null,
  email          citext not null unique,
  password_hash  text not null,           -- bcrypt/argon2 — nunca hash simples
  role           user_role not null default 'member',
  is_active      boolean not null default true,   -- desativa o assento sem perder o histórico de quem fez o quê
  last_login_at  timestamptz,
  created_at     timestamptz not null default now(),
  updated_at     timestamptz not null default now(),

  unique (id, company_id)   -- alvo das FKs compostas
);

create index idx_users_company on users(company_id) where is_active;

create trigger trg_users_touch before update on users
  for each row execute function touch_updated_at();

-- Limite de assentos como regra do banco, não só do backend. O advisory lock
-- serializa por empresa: sem ele, dois convites aceitos ao mesmo tempo passariam
-- pela contagem juntos e estourariam o limite.
create or replace function enforce_seat_limit() returns trigger as $$
declare
  active_seats int;
  max_seats    int;
begin
  if not new.is_active then
    return new;   -- desativar assento nunca estoura limite
  end if;

  perform pg_advisory_xact_lock(hashtext(new.company_id::text));

  select count(*) into active_seats
    from users
   where company_id = new.company_id and is_active and id <> new.id;

  select seat_limit into max_seats from companies where id = new.company_id;

  if active_seats >= max_seats then
    raise exception 'Empresa % já ocupa seus % assentos de RH ativos', new.company_id, max_seats
      using errcode = 'check_violation';
  end if;

  return new;
end;
$$ language plpgsql;

create trigger trg_users_seat_limit
  before insert or update of is_active, company_id on users
  for each row execute function enforce_seat_limit();

-- Convite pro segundo assento de RH. O 'owner' cria o convite; a pessoa
-- convidada usa o token pra criar a própria senha e vira `users` com role='member'.
create type invitation_status as enum ('pending', 'accepted', 'expired', 'revoked');

create table user_invitations (
  id                 uuid primary key default gen_random_uuid(),
  company_id         uuid not null references companies(id) on delete cascade,
  email              citext not null,
  invited_by_user_id uuid not null,
  token_hash         text not null unique,
  status             invitation_status not null default 'pending',
  expires_at         timestamptz not null,
  accepted_at        timestamptz,
  created_at         timestamptz not null default now(),

  foreign key (invited_by_user_id, company_id) references users (id, company_id)
);

create index idx_invitations_company on user_invitations(company_id);
create index idx_invitations_inviter on user_invitations(invited_by_user_id);
-- Um convite pendente por e-mail por empresa.
create unique index idx_invitations_pending_unique
  on user_invitations(company_id, email) where status = 'pending';
create index idx_invitations_expiry on user_invitations(expires_at) where status = 'pending';

-- Sessão = access token JWT de vida curta (não persistido) + refresh token
-- (persistido, revogável) — permite "sair de todos os dispositivos" e detectar
-- reuso de token roubado.
create table refresh_tokens (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references users(id) on delete cascade,
  token_hash  text not null unique,
  user_agent  text,
  ip_address  inet,
  created_at  timestamptz not null default now(),
  expires_at  timestamptz not null,
  revoked_at  timestamptz
);

create index idx_refresh_tokens_user on refresh_tokens(user_id);
create index idx_refresh_tokens_expiry on refresh_tokens(expires_at) where revoked_at is null;

create table password_reset_tokens (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references users(id) on delete cascade,
  token_hash  text not null unique,
  expires_at  timestamptz not null,
  used_at     timestamptz,
  created_at  timestamptz not null default now()
);

create index idx_reset_tokens_user on password_reset_tokens(user_id);
create index idx_reset_tokens_expiry on password_reset_tokens(expires_at) where used_at is null;


-- ============================================================================
-- 2. TAXONOMIA DE COMPETÊNCIAS (seção 7 do documento de especificação)
-- ============================================================================
-- "SAP FI" = "SAP Finance" = "FI/CO" = "módulo financeiro SAP" precisam
-- resolver pro MESMO registro, senão o filtro estruturado erra silenciosamente.
-- Por isso os termos são `citext`: "SAP FI" e "sap fi" não podem coexistir como
-- dois canônicos — seria a tabela derrotando o próprio propósito.
--
-- A taxonomia é GLOBAL (ativo do produto, atravessa clientes). O texto bruto que
-- gerou uma dúvida de mapeamento, não: esse fica preso à empresa de origem.

create table skills (
  id             uuid primary key default gen_random_uuid(),
  canonical_term citext not null unique,
  category       text,             -- ex.: 'SAP', 'Frontend', 'Dados' — livre, sem enum fechado
  created_at     timestamptz not null default now()
);

create table skill_synonyms (
  id            uuid primary key default gen_random_uuid(),
  skill_id      uuid not null references skills(id) on delete cascade,
  synonym_text  citext not null unique   -- um sinônimo pertence a exatamente um termo canônico
);

create index idx_skill_synonyms_skill on skill_synonyms(skill_id);

-- Um termo não pode ser canônico de uma skill e sinônimo de outra ao mesmo
-- tempo — a resolução ficaria ambígua e o filtro estruturado erraria em silêncio.
create or replace function enforce_term_uniqueness() returns trigger as $$
begin
  if tg_table_name = 'skills' then
    if exists (select 1 from skill_synonyms where synonym_text = new.canonical_term) then
      raise exception 'Termo "%" já existe como sinônimo de outra competência', new.canonical_term
        using errcode = 'unique_violation';
    end if;
  else
    if exists (select 1 from skills where canonical_term = new.synonym_text) then
      raise exception 'Termo "%" já existe como termo canônico', new.synonym_text
        using errcode = 'unique_violation';
    end if;
  end if;
  return new;
end;
$$ language plpgsql;

create trigger trg_skills_term_unique before insert or update on skills
  for each row execute function enforce_term_uniqueness();
create trigger trg_synonyms_term_unique before insert or update on skill_synonyms
  for each row execute function enforce_term_uniqueness();

-- Termo que a IA não conseguiu mapear na ingestão (seção 7: "vão para uma fila
-- de revisão"). `raw_text` é trecho de currículo de um cliente — por isso a fila
-- é escopada por empresa, mesmo a taxonomia sendo global.
create type skill_mapping_status as enum ('pending', 'mapped', 'rejected');

create table skill_mapping_review_queue (
  id                 uuid primary key default gen_random_uuid(),
  company_id         uuid not null references companies(id) on delete cascade,
  raw_text           text not null,
  suggested_skill_id uuid references skills(id),
  status             skill_mapping_status not null default 'pending',
  created_at         timestamptz not null default now(),
  resolved_at        timestamptz
);

create index idx_skill_queue_pending on skill_mapping_review_queue(company_id) where status = 'pending';
create index idx_skill_queue_suggested on skill_mapping_review_queue(suggested_skill_id);

create table sectors (
  id    uuid primary key default gen_random_uuid(),
  name  citext not null unique
);

create table languages (
  id    uuid primary key default gen_random_uuid(),
  name  citext not null unique
);


-- ============================================================================
-- 3. TALENTOS (pessoa, global por empresa cliente — seções 3 e 4 do doc)
-- ============================================================================
-- INVARIANTE (seções 6.3 e 11 do doc): "O banco não deve, em hipótese alguma,
-- persistir o score como atributo do talento." Não existe coluna de match/score
-- aqui, e não deve existir nunca — o match só faz sentido contra os critérios de
-- UMA vaga por vez e é recalculado a cada campanha nova.

create type talent_origin as enum ('reprovacao_qualificada', 'cadastro_manual', 'importacao');
create type legal_basis as enum ('consentimento', 'legitimo_interesse', 'a_avaliar');
create type consent_state as enum ('consentido', 'nao_notificado', 'notificado', 'oposicao_exclusao');

create table talents (
  id                     uuid primary key default gen_random_uuid(),
  company_id             uuid not null references companies(id) on delete cascade,
  name                   text not null,
  email                  citext,     -- ajuda deduplicação futura; hoje o produto não resolve identidade entre fontes
  phone                  text,
  city                   text,
  state                  text,
  modality               text,       -- livre (não enum): talento pode aceitar mais de uma modalidade
  seniority              text,       -- livre: valores compostos existem hoje ("Pleno-Sênior")
  years_experience       int,
  salary_min             numeric(10,2),
  salary_max             numeric(10,2),
  salary_currency        text not null default 'BRL',
  available_from         date,
  availability_note      text,       -- ex.: "A combinar" — nem toda disponibilidade cabe numa data
  origin                 talent_origin not null,
  legal_basis            legal_basis not null,
  consent_state          consent_state not null default 'nao_notificado',
  consent_date           date,
  summary                text,
  recruiter_notes        text,
  education_degree       text,
  education_institution  text,
  education_period       text,
  -- Embedding do perfil pra busca semântica (seção 6.1/6.2). Dimensão depende do
  -- modelo escolhido — 1536 é o padrão pra text-embedding-3-small.
  embedding              vector(1536),

  -- DUAS DATAS DIFERENTES, DE PROPÓSITO:
  --   updated_at         = a linha mudou (mantido por trigger, uso técnico).
  --   profile_reviewed_at = alguém de fato revisou/enriqueceu ESTE perfil.
  -- O selo de frescor da tela (src/app/core/talent-view.ts) lê o segundo. Se
  -- lesse o primeiro, um job que só recalcula embedding rejuvenesceria o perfil
  -- sem ninguém ter falado com a pessoa — exatamente o risco "envelhecimento de
  -- perfil" da seção 9 do documento, mascarado em vez de mitigado.
  profile_reviewed_at    timestamptz not null default now(),

  created_at             timestamptz not null default now(),
  updated_at             timestamptz not null default now(),
  deleted_at             timestamptz,

  unique (id, company_id)   -- alvo das FKs compostas
);

create index idx_talents_company on talents(company_id) where deleted_at is null;
create index idx_talents_embedding on talents using hnsw (embedding vector_cosine_ops);
create index idx_talents_freshness on talents(company_id, profile_reviewed_at) where deleted_at is null;

create trigger trg_talents_touch before update on talents
  for each row execute function touch_updated_at();

create table talent_experience_entries (
  id            uuid primary key default gen_random_uuid(),
  talent_id     uuid not null references talents(id) on delete cascade,
  role          text not null,
  company       text not null,
  period_label  text not null,
  description   text,
  position      int not null default 0
);

create index idx_talent_experience_talent on talent_experience_entries(talent_id);

create table talent_skills (
  id                uuid primary key default gen_random_uuid(),
  talent_id         uuid not null references talents(id) on delete cascade,
  skill_id          uuid not null references skills(id),
  level             text,
  years_experience  int,
  unique (talent_id, skill_id)
);

-- Mapa de cobertura e busca por competência varrem por skill_id — é o caminho
-- quente de duas das quatro ferramentas do Banco de Talentos (seção 8 do doc).
create index idx_talent_skills_skill on talent_skills(skill_id);

create table talent_sectors (
  id         uuid primary key default gen_random_uuid(),
  talent_id  uuid not null references talents(id) on delete cascade,
  sector_id  uuid not null references sectors(id),
  unique (talent_id, sector_id)
);

create index idx_talent_sectors_sector on talent_sectors(sector_id);

create table talent_languages (
  id            uuid primary key default gen_random_uuid(),
  talent_id     uuid not null references talents(id) on delete cascade,
  language_id   uuid not null references languages(id),
  proficiency   text,   -- 'básico' | 'intermediário' | 'avançado' — livre, varia por idioma/contexto
  unique (talent_id, language_id)
);

create index idx_talent_languages_language on talent_languages(language_id);

-- Material bruto por trás do perfil estruturado (seção 6.1: "persiste-se o perfil
-- estruturado, o embedding E o material não estruturado"). Sem isso não há como
-- reprocessar um perfil se a extração da IA melhorar, nem auditar a origem do dado.
create type talent_source_kind as enum ('resume', 'interview_feedback', 'recruiter_note', 'import_row');

create table talent_source_documents (
  id          uuid primary key default gen_random_uuid(),
  talent_id   uuid not null references talents(id) on delete cascade,
  kind        talent_source_kind not null,
  raw_text    text not null,
  created_at  timestamptz not null default now()
);

create index idx_talent_sources_talent on talent_source_documents(talent_id);


-- ============================================================================
-- 4. CAMPANHAS (VAGAS)
-- ============================================================================

create type campaign_status as enum ('ativa', 'pausada', 'encerrada');
create type work_modality as enum ('remoto', 'hibrido', 'presencial');
create type contract_type as enum ('clt', 'pj', 'estagio');
create type seniority_level as enum ('junior', 'pleno', 'senior');
create type phase_key as enum ('recebidos', 'fit', 'tecnica', 'entrevista', 'selecionados');

create table campaigns (
  id                 uuid primary key default gen_random_uuid(),
  company_id         uuid not null references companies(id) on delete cascade,
  created_by_user_id uuid not null,
  title              text not null,
  city               text,
  state              text,
  modality           work_modality not null,
  contract_type      contract_type not null,
  seniority          seniority_level not null,
  status             campaign_status not null default 'ativa',
  opened_at          date not null default current_date,
  paused_at          timestamptz,
  closed_at          timestamptz,
  created_at         timestamptz not null default now(),
  updated_at         timestamptz not null default now(),
  deleted_at         timestamptz,

  -- Quem criou tem que ser RH DESTA empresa.
  foreign key (created_by_user_id, company_id) references users (id, company_id),
  unique (id, company_id)   -- alvo das FKs compostas
);

create index idx_campaigns_company on campaigns(company_id) where deleted_at is null;
create index idx_campaigns_creator on campaigns(created_by_user_id);

create trigger trg_campaigns_touch before update on campaigns
  for each row execute function touch_updated_at();

-- Quais módulos de funil essa campanha usa e em que ordem ("Recebidos" e
-- "Selecionados" são fixos; Fit/Técnica/Entrevista são escolhidos na criação —
-- ver tela Nova Campanha, step 3).
create table campaign_phases (
  id           uuid primary key default gen_random_uuid(),
  campaign_id  uuid not null references campaigns(id) on delete cascade,
  phase_key    phase_key not null,
  position     int not null,

  unique (campaign_id, phase_key),
  -- DEFERRABLE porque a tela reordena fases trocando posições entre si: sem
  -- adiar a checagem pro fim da transação, o primeiro UPDATE do swap já colide.
  unique (campaign_id, position) deferrable initially deferred
);

-- Critérios estruturados de match da vaga (seção 6.3: "o score é uma fórmula com
-- pesos aplicada sobre os critérios da vaga"). Hoje o protótipo deriva isso do
-- título em texto livre (core/talent-matching.ts); esta tabela é a versão real.
create table campaign_match_criteria (
  id             uuid primary key default gen_random_uuid(),
  campaign_id    uuid not null references campaigns(id) on delete cascade,
  criterion_type text not null,   -- 'skill' | 'sector' | 'modality' | 'seniority' | 'language' | 'salary_max' | 'availability_max_days'
  skill_id       uuid references skills(id),   -- só quando criterion_type = 'skill'
  value_text     text,                          -- valor livre pros demais tipos
  weight         int not null,
  required       boolean not null default false,

  constraint skill_criterion_needs_skill
    check ((criterion_type = 'skill') = (skill_id is not null))
);

create index idx_campaign_criteria_campaign on campaign_match_criteria(campaign_id);
create index idx_campaign_criteria_skill on campaign_match_criteria(skill_id);


-- ============================================================================
-- 5. CANDIDATOS (participação numa campanha específica)
-- ============================================================================
-- "Candidato é papel relativo a uma vaga" (seção 3 do doc). Vive só dentro de
-- uma campanha — quando o motivo de reprovação qualifica, um Talento nasce ou é
-- ligado (talent_id), mas o Candidato nunca "vira" Talento por inteiro.

create type candidate_status as enum (
  'aguardando_triagem_ia', 'em_analise', 'aguardando_decisao', 'reprovado', 'proposta', 'contratado'
);

-- Espelha a Tabela 1 do documento — fonte única de verdade pro motivo de
-- reprovação, inclusive se ele qualifica ou não pro banco.
create table rejection_reasons (
  key           text primary key,   -- 'perdeu_outro_candidato', 'faltou_skill', ...
  label         text not null,
  goes_to_bank  boolean not null,
  effect_label  text not null,
  position      int not null
);

create table candidates (
  id                     uuid primary key default gen_random_uuid(),
  -- Redundante com campaigns.company_id de propósito: é o que permite a FK
  -- composta abaixo barrar vínculo entre empresas diferentes, e evita um join
  -- só pra descobrir o tenant numa política de RLS.
  company_id             uuid not null references companies(id) on delete cascade,
  campaign_id            uuid not null,
  talent_id              uuid,       -- setado quando a reprovação qualificada cria/liga um talento
  name                   text not null,
  email                  citext,
  phone                  text,
  linkedin_url           text,
  city                   text,
  state                  text,
  years_experience       int,
  phase_key              phase_key not null default 'recebidos',
  status                 candidate_status not null default 'aguardando_triagem_ia',
  rejection_reason_key   text references rejection_reasons(key),
  rejected_at            timestamptz,
  summary                text,
  education_degree       text,
  education_institution  text,
  education_period       text,
  created_at             timestamptz not null default now(),
  updated_at             timestamptz not null default now(),
  deleted_at             timestamptz,

  foreign key (campaign_id, company_id) references campaigns (id, company_id),
  foreign key (talent_id, company_id)   references talents  (id, company_id),
  -- A fase do candidato tem que existir no funil configurado DESTA campanha:
  -- sem isso um candidato podia sentar numa etapa que a vaga nem tem.
  foreign key (campaign_id, phase_key) references campaign_phases (campaign_id, phase_key),

  constraint rejection_fields_match_status
    check ((status = 'reprovado') = (rejection_reason_key is not null)),
  constraint rejected_at_matches_status
    check ((status = 'reprovado') = (rejected_at is not null)),

  unique (id, company_id)   -- alvo das FKs compostas
);

create index idx_candidates_campaign on candidates(campaign_id) where deleted_at is null;
create index idx_candidates_talent on candidates(talent_id) where talent_id is not null;
create index idx_candidates_company on candidates(company_id) where deleted_at is null;
create index idx_candidates_reason on candidates(rejection_reason_key);

create trigger trg_candidates_touch before update on candidates
  for each row execute function touch_updated_at();

create table candidate_experience_entries (
  id            uuid primary key default gen_random_uuid(),
  candidate_id  uuid not null references candidates(id) on delete cascade,
  role          text not null,
  company       text not null,
  period_label  text not null,
  description   text,
  position      int not null default 0
);

create index idx_candidate_experience_candidate on candidate_experience_entries(candidate_id);

create table candidate_skills (
  id                uuid primary key default gen_random_uuid(),
  candidate_id      uuid not null references candidates(id) on delete cascade,
  skill_id          uuid not null references skills(id),
  level             text,
  years_experience  int,
  unique (candidate_id, skill_id)
);

create index idx_candidate_skills_skill on candidate_skills(skill_id);

-- Avaliação da IA pra ESTA fase específica. Fica histórico — quando o candidato
-- avança, uma nova assessment é criada, a anterior não é sobrescrita. É o que dá
-- a trilha de "o que a IA dizia quando aprovamos essa pessoa".
create table candidate_ai_assessments (
  id             uuid primary key default gen_random_uuid(),
  candidate_id   uuid not null references candidates(id) on delete cascade,
  phase_key      phase_key not null,
  match_pct      int not null check (match_pct between 0 and 100),
  match_label    text,
  match_note     text,
  justification  text,
  created_at     timestamptz not null default now()
);

-- "Última avaliação deste candidato" é a leitura mais comum (listagem de fase
-- ordenada por match) — por isso o índice já vem ordenado por data desc.
create index idx_ai_assessments_latest on candidate_ai_assessments(candidate_id, created_at desc);

create type ai_assessment_point_kind as enum ('strength', 'concern');

create table candidate_ai_assessment_points (
  id             uuid primary key default gen_random_uuid(),
  assessment_id  uuid not null references candidate_ai_assessments(id) on delete cascade,
  kind           ai_assessment_point_kind not null,
  text           text not null,
  position       int not null default 0
);

create index idx_assessment_points_assessment on candidate_ai_assessment_points(assessment_id);


-- ============================================================================
-- 6. DECISÕES — o evento de domínio do produto
-- ============================================================================
-- "A IA sugere, o humano decide" é a tese do produto, e a decisão precisava de
-- tabela própria. Sem ela o banco só guarda o ESTADO FINAL do candidato: não dá
-- pra saber quantas decisões manuais houve, quando, por qual dos dois RHs, nem
-- contra qual recomendação da IA — e portanto a métrica "Aderência às
-- recomendações da IA" da tela Relatórios não seria computável de verdade
-- (só por parsing de texto livre do activity_feed, que é o oposto de auditável).

create type decision_kind as enum ('avancar', 'reprovar');

create table candidate_decisions (
  id                   uuid primary key default gen_random_uuid(),
  company_id           uuid not null references companies(id) on delete cascade,
  candidate_id         uuid not null,
  decided_by_user_id   uuid not null,
  -- A avaliação que estava na tela quando a pessoa decidiu. É o par
  -- (recomendação × decisão) que torna a métrica de confiança calculável.
  assessment_id        uuid references candidate_ai_assessments(id),
  decision             decision_kind not null,
  from_phase           phase_key not null,
  to_phase             phase_key,                                  -- null quando decision = 'reprovar'
  rejection_reason_key text references rejection_reasons(key),
  created_at           timestamptz not null default now(),

  foreign key (candidate_id, company_id)       references candidates (id, company_id),
  foreign key (decided_by_user_id, company_id) references users      (id, company_id),

  constraint decision_shape
    check (
      (decision = 'avancar'  and to_phase is not null and rejection_reason_key is null) or
      (decision = 'reprovar' and to_phase is null     and rejection_reason_key is not null)
    )
);

create index idx_decisions_candidate on candidate_decisions(candidate_id, created_at desc);
create index idx_decisions_company on candidate_decisions(company_id, created_at desc);
create index idx_decisions_user on candidate_decisions(decided_by_user_id);
create index idx_decisions_assessment on candidate_decisions(assessment_id);
create index idx_decisions_reason on candidate_decisions(rejection_reason_key);


-- ============================================================================
-- 7. NOTIFICAÇÕES E ATIVIDADE
-- ============================================================================

create type actor_type as enum ('ia', 'recrutador');

-- Uma linha por (evento, destinatário) — com só 2 assentos por empresa, o
-- fan-out é trivial e mantém "não lidas do usuário X" como query direta.
create table notifications (
  id             uuid primary key default gen_random_uuid(),
  company_id     uuid not null references companies(id) on delete cascade,
  user_id        uuid not null,              -- destinatário
  actor          actor_type not null,
  actor_user_id  uuid,                       -- quem gerou, quando actor = 'recrutador'
  campaign_id    uuid,
  candidate_id   uuid,
  talent_id      uuid,
  message        text not null,
  route          text,                       -- rota de destino no front
  read_at        timestamptz,
  created_at     timestamptz not null default now(),

  -- Toda referência confere o tenant: sem isso uma notificação da Empresa A
  -- podia ser entregue ao RH da Empresa B.
  foreign key (user_id, company_id)       references users      (id, company_id),
  foreign key (actor_user_id, company_id) references users      (id, company_id),
  foreign key (campaign_id, company_id)   references campaigns  (id, company_id),
  foreign key (candidate_id, company_id)  references candidates (id, company_id),
  foreign key (talent_id, company_id)     references talents    (id, company_id),

  constraint actor_user_matches_actor
    check ((actor = 'recrutador') or actor_user_id is null)
);

create index idx_notifications_user_unread on notifications(user_id) where read_at is null;
create index idx_notifications_company on notifications(company_id, created_at desc);
create index idx_notifications_actor on notifications(actor_user_id);
create index idx_notifications_campaign on notifications(campaign_id);
create index idx_notifications_candidate on notifications(candidate_id);
create index idx_notifications_talent on notifications(talent_id);

-- Trilha compartilhada da empresa — não é caixa de entrada pessoal, é o feed
-- único da tela Atividade. Com `candidate_decisions` existindo, boa parte deste
-- feed passa a ser derivável em vez de escrita à mão como texto livre.
create table activity_feed (
  id             uuid primary key default gen_random_uuid(),
  company_id     uuid not null references companies(id) on delete cascade,
  campaign_id    uuid,
  actor          actor_type not null,
  actor_user_id  uuid,
  message        text not null,
  created_at     timestamptz not null default now(),

  foreign key (campaign_id, company_id)   references campaigns (id, company_id),
  foreign key (actor_user_id, company_id) references users     (id, company_id)
);

create index idx_activity_company on activity_feed(company_id, created_at desc);
create index idx_activity_campaign on activity_feed(campaign_id);
create index idx_activity_actor on activity_feed(actor_user_id);

create table ai_suggestions (
  id                    uuid primary key default gen_random_uuid(),
  company_id            uuid not null references companies(id) on delete cascade,
  campaign_id           uuid,
  message               text not null,
  primary_action_label  text not null,
  primary_action_route  text not null,
  highlighted           boolean not null default false,
  dismissed_at          timestamptz,
  created_at            timestamptz not null default now(),

  foreign key (campaign_id, company_id) references campaigns (id, company_id)
);

create index idx_ai_suggestions_company on ai_suggestions(company_id) where dismissed_at is null;
create index idx_ai_suggestions_campaign on ai_suggestions(campaign_id);


-- ============================================================================
-- MÉTRICAS COMPUTADAS — de propósito, NÃO SÃO TABELAS
-- ============================================================================
-- Tudo que no protótipo já é "calculado no MockApiService, nunca hardcodado
-- solto" continua assim aqui — vira query, nunca coluna. Guardar qualquer um
-- desses valores cria uma segunda fonte de verdade que diverge da primeira.
--
--   Phase.count (funil por campanha)
--     select phase_key, count(*) from candidates
--     where campaign_id = :id and deleted_at is null group by phase_key;
--
--   Campaign.totalCandidates / funnelPercent / currentPhaseLabel
--     agregação sobre `candidates`, não coluna em `campaigns`.
--
--   Match mais recente de cada candidato (listagem de fase ordenada por match)
--     select c.*, a.match_pct
--     from candidates c
--     left join lateral (
--       select match_pct from candidate_ai_assessments
--       where candidate_id = c.id order by created_at desc limit 1
--     ) a on true
--     where c.campaign_id = :id and c.deleted_at is null;
--     -- Existiu aqui uma coluna `latest_match_pct` como cache. Foi removida:
--     -- cache sem trigger de sincronia é a mesma classe de bug que o updated_at
--     -- congelado, e contradiz o princípio declarado no topo deste arquivo.
--
--   AiTrustMetrics (tela Relatórios — "Confiança na IA")
--     agora sai de `candidate_decisions` × `candidate_ai_assessments`:
--     select
--       count(*) as decisions_analyzed,
--       count(*) filter (where (d.decision = 'avancar'  and a.match_pct >= 70)
--                           or (d.decision = 'reprovar' and a.match_pct <  70))
--         * 100.0 / nullif(count(*), 0) as agreement_rate_pct,
--       count(*) filter (where d.decision = 'avancar'  and a.match_pct <  70) as overridden_approvals,
--       count(*) filter (where d.decision = 'reprovar' and a.match_pct >= 70) as overridden_rejections
--     from candidate_decisions d
--     join candidate_ai_assessments a on a.id = d.assessment_id
--     where d.company_id = :id and d.created_at >= now() - interval '30 days';
--     -- (o corte de 70 é o limiar de "recomendado" — parametrizar quando a
--     --  fórmula de score estiver fechada.)
--
--   Mapa de cobertura do banco (CoverageEntry)
--     select s.canonical_term, count(*) from talent_skills ts
--     join skills s on s.id = ts.skill_id
--     join talents t on t.id = ts.talent_id
--     where t.company_id = :id and t.consent_state <> 'oposicao_exclusao'
--       and t.deleted_at is null
--     group by s.canonical_term order by count(*) desc;
--
--   TalentMatch (busca, encontrar parecidos, match reverso)
--     fórmula com pesos sobre talent_skills/talent_sectors/... comparados aos
--     critérios da busca ou de campaign_match_criteria — nunca persistida.
--     A parte semântica usa `talents.embedding` com pgvector; os filtros duros
--     são WHERE em SQL sobre campos indexados.
--
--   Talent.history (histórico em campanhas)
--     select c.campaign_id, camp.title, c.phase_key, c.status, c.rejection_reason_key
--     from candidates c join campaigns camp on camp.id = c.campaign_id
--     where c.talent_id = :talentId order by c.updated_at desc;
--     -- Não existe tabela `talent_history`.
-- ============================================================================


-- ============================================================================
-- AINDA EM ABERTO (decisão de produto/jurídica, não de engenharia)
--
-- 1. Exclusão solicitada (oposicao_exclusao): o schema mantém a linha em
--    `talents`, filtrada por consent_state nas queries. Uma política real de
--    retenção pode exigir anonimizar (null em name/email/phone/summary/
--    recruiter_notes) em vez de só bloquear o uso. O schema já facilita: PII
--    está em colunas próprias e o material bruto isolado em
--    `talent_source_documents`, que pode ser apagado sem destruir o histórico
--    agregado.
--
-- 2. Deduplicação de identidade: a mesma pessoa pode existir como `candidates`
--    em duas campanhas sem ligação entre si. `email` dá a chave natural pra um
--    matching futuro; a lógica em si não está aqui, de propósito.
--
-- 3. `seat_limit` em `companies` funciona com um plano só. Havendo mais de um
--    preço, migra pra `plans`/`subscriptions`.
--
-- 4. Row-Level Security: as FKs compostas garantem COERÊNCIA (nada aponta pra
--    outra empresa), mas não impedem uma query sem WHERE de LER outra empresa.
--    Se o backend for multi-tenant num banco só, vale ligar RLS por company_id —
--    agora é barato, porque toda tabela relevante já carrega a coluna.
--
-- 5. O limiar de "match recomendado" (70 no exemplo da métrica de confiança)
--    precisa sair da fórmula de score definitiva, não de um número solto.
-- ============================================================================
