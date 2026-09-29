-- Etapa 2 do match reverso (documentos/banco-de-talentos-recomendacao-plano.md): leitura da IA
-- sob demanda sobre um recorte pequeno (top-5) do ranking determinístico já existente
-- (talent.Service.ReverseMatch). Nunca automática, nunca sobre o banco inteiro — sempre uma ação
-- explícita do recrutador sobre talentos que ele já revisou.
--
-- Estrutura espelha candidate_ai_assessments (mesma ideia: linha imutável por evento de avaliação,
-- histórico não sobrescrito), com as colunas de stage-aware assessment (confidence,
-- missing_information) já de origem — esta tabela nasce depois de migrations/0013, não precisa da
-- evolução em duas etapas que aquela tabela precisou.
--
-- NÃO viola o invariante "nenhum score persistido como atributo do talento" (migrations/0001,
-- comentário da seção 3 "TALENTOS", e documento de especificação do Banco de Talentos, seção
-- 6.3/11): não é uma coluna em `talents`, é um evento de avaliação escopado a UMA campanha — a
-- mesma distinção que já justifica candidate_ai_assessments existir apesar do mesmo invariante
-- valer para `candidates`. O talento continua sem NENHUM campo de match; o que existe aqui é
-- "o que a IA achou de X para a vaga Y, em tal data", nunca "o match atual de X".
--
-- strengths/concerns/missing_information como text[] direto na tabela, não uma tabela filha de
-- pontos (diferente de candidate_ai_assessment_points): esta tabela nasce depois de
-- migrations/0013 ter mostrado que o padrão mais simples (array direto, como missing_information
-- lá) resolve igual sem uma segunda tabela — não há motivo pra repetir o padrão mais pesado numa
-- tabela nova.
create table campaign_talent_recommendations (
  id                  uuid primary key default gen_random_uuid(),
  company_id          uuid not null references companies(id) on delete cascade,
  campaign_id         uuid not null,
  talent_id           uuid not null,
  match_pct           int not null check (match_pct between 0 and 100),
  match_label         text,
  match_note          text,
  justification       text,
  strengths           text[] not null default '{}',
  concerns            text[] not null default '{}',
  confidence          text not null check (confidence in ('alta', 'media', 'baixa', 'insuficiente')),
  missing_information text[] not null default '{}',
  provider            text not null,
  model               text,
  -- prompt_version é o que autoriza reavaliar (mesma regra de candidate_ai_assessments): pedir de
  -- novo para o mesmo (campaign_id, talent_id) com a MESMA versão de prompt devolve esta linha sem
  -- chamar o provedor; mudar o prompt muda a versão e libera uma avaliação nova.
  prompt_version      text not null,
  created_at          timestamptz not null default now(),

  foreign key (campaign_id, company_id) references campaigns (id, company_id),
  foreign key (talent_id, company_id)   references talents   (id, company_id)
);

-- Um evento por (campanha, talento, versão do prompt) — é o que torna "pedir de novo" idempotente
-- e barato (ver AssessForCampaign em internal/domain/candidate/service.go).
create unique index uq_campaign_talent_reco_version
  on campaign_talent_recommendations (campaign_id, talent_id, prompt_version);

-- "Já avaliamos algum talento pra esta campanha?" é o caminho de leitura mais comum (montar o
-- painel "Sugestões do Banco de Talentos" na tela de Candidatos) — por isso ordenado por data.
create index idx_campaign_talent_reco_campaign
  on campaign_talent_recommendations (campaign_id, created_at desc);
