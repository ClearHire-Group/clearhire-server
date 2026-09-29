-- Registro de uso de LLM: uma linha por chamada ao provedor.
--
-- Antes disto o `usage` devolvido pela API era descartado, e a única forma de saber quanto a IA
-- custava era esperar a fatura. Esta tabela é o que permite responder, a qualquer momento:
--   "quanto esta campanha custou de IA?"
--   "quanto custa em média processar um candidato?"
--   "qual etapa consome mais tokens?"
--   "qual modelo está gerando mais custo?"
create type llm_call_status as enum (
  'success',    -- resposta válida, cobrada
  'billed_error', -- resposta CHEGOU e foi cobrada, mas era inútil (truncada, recusada, ilegível)
  'failed',     -- não houve resposta (rede, 5xx, rate limit) — normalmente não cobrada
  'cache_hit'   -- não houve chamada: o conteúdo já tinha sido extraído antes
);

create table llm_usage (
  id           uuid primary key default gen_random_uuid(),
  company_id   uuid not null references companies(id) on delete cascade,

  -- Nuláveis de propósito. A extração acontece ANTES da transação que cria candidato e talento
  -- (chamada de rede de dezenas de segundos não pode segurar transação aberta), então no instante
  -- em que o gasto ocorre esses ids ainda não existem. Preferir registrar o gasto sem eles a
  -- registrar depois: ver a nota sobre rollback abaixo.
  campaign_id  uuid,
  candidate_id uuid,

  operation    text not null,             -- 'extraction' hoje; 'assessment' quando a fase 5 chegar
  provider     text not null,             -- 'anthropic' | 'gemini' | 'deterministic'
  model        text not null default '',  -- vazio quando não houve modelo (determinístico, cache)
  prompt_version text not null default '',

  input_tokens        int not null default 0,
  output_tokens       int not null default 0,
  cached_input_tokens int not null default 0,

  -- NULL = preço do modelo desconhecido; 0 = foi realmente de graça. A distinção importa: tratar
  -- desconhecido como zero esconderia justamente o gasto que esta tabela existe para expor.
  estimated_cost_usd numeric(12, 6),

  status       llm_call_status not null,
  error_code   text,
  duration_ms  int not null default 0,
  -- Liga o gasto ao documento que o causou, permitindo detectar reenvio abusivo do mesmo currículo.
  fingerprint  text,
  created_at   timestamptz not null default now()
);

-- Nota de desenho (a regra que mais importa aqui): esta linha é gravada FORA da transação que cria
-- candidato/talento, logo após a chamada ao provedor. É intencional. O dinheiro foi gasto quer a
-- candidatura seja concluída ou não — se o registro do gasto participasse da mesma transação, um
-- rollback apagaria a prova de uma cobrança que o provedor vai fazer de qualquer jeito, e a
-- contabilidade passaria a subestimar exatamente nos casos de falha.

create index idx_llm_usage_company_date on llm_usage(company_id, created_at desc);
create index idx_llm_usage_campaign on llm_usage(campaign_id) where campaign_id is not null;
create index idx_llm_usage_fingerprint on llm_usage(company_id, fingerprint) where fingerprint is not null;
