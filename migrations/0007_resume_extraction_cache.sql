-- Cache de extração de currículo, endereçado por conteúdo.
--
-- Porquê: a extração é a única etapa paga do pipeline hoje, e antes desta tabela ela rodava de
-- novo a cada envio — inclusive quando o MESMO arquivo voltava. Duas situações reais em que isso
-- queimava dinheiro sem produzir nada novo:
--   1. a pessoa reenvia o formulário (rede caiu, clicou duas vezes, corrigiu um campo);
--   2. a mesma pessoa se candidata a outra vaga da mesma empresa com o mesmo currículo.
-- O custo era linear no número de ENVIOS, quando deveria ser linear no número de DOCUMENTOS
-- distintos.
--
-- A chave é o sha256 do conteúdo normalizado (ver llm.Fingerprint), não o e-mail nem o candidato:
-- o que determina o resultado da extração é o documento, nada mais.
create table resume_extractions (
  id          uuid primary key default gen_random_uuid(),
  company_id  uuid not null references companies(id) on delete cascade,
  -- sha256 em hex do texto normalizado (ou dos bytes, quando é PDF escaneado sem camada de texto).
  fingerprint text not null,
  -- llm.ExtractedProfile serializado. Guardado como documento de propósito: é o retorno cru do
  -- extrator, não dado de domínio normalizado — quem normaliza são as tabelas talent_*. Misturar
  -- os dois faria esta tabela precisar migrar toda vez que o schema de extração mudasse.
  profile     jsonb not null,
  created_at  timestamptz not null default now(),

  -- Escopo por empresa mesmo que o hash seja globalmente determinístico: currículo é dado pessoal
  -- de um tenant. Cache compartilhado entre empresas vazaria a EXISTÊNCIA de um candidato de uma
  -- empresa para outra (um hit revelaria que aquele CV já passou por outro lugar).
  unique (company_id, fingerprint)
);

-- Trocar de modelo NÃO invalida este cache automaticamente, de propósito: reprocessar é ação
-- deliberada, nunca efeito colateral silencioso de um deploy (um invalidate automático re-extrairia
-- a base inteira e a conta apareceria sem ninguém ter pedido). Para reprocessar, apague as linhas
-- do escopo desejado.
