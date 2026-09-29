-- Teto de gasto com LLM por empresa, por mês civil.
--
-- Existe porque a rota de candidatura pública é ANÔNIMA: qualquer pessoa com o link dispara a única
-- etapa paga do sistema. O rate limit por IP não resolve isso sozinho — ele é por IP, em memória e
-- por instância, então N origens multiplicam o gasto linearmente e nada no sistema perceberia.
-- Este teto é o limite que não depende de adivinhar de onde vem o tráfego.
--
-- Mês CIVIL, e não janela deslizante, porque é assim que o provedor fatura: o número aqui tem que
-- ser comparável ao da fatura sem tradução mental.
alter table companies
  add column llm_monthly_budget_usd numeric(10, 2) not null default 10.00
    check (llm_monthly_budget_usd >= 0);

comment on column companies.llm_monthly_budget_usd is
  'Teto de gasto com provedor de LLM no mês civil. Ao ser atingido, a extração por IA é RECUSADA '
  '(o candidato é direcionado ao formulário manual, que é gratuito) — bloqueia de fato, não apenas '
  'alerta. 0 desliga a extração por IA para a empresa. Extração servida do cache não consome '
  'orçamento, porque não gera chamada ao provedor.';
