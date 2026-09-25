-- 1. Unicidade de candidatura por vaga + e-mail, garantida pelo banco.
--
-- Até aqui a única barreira era um "existe?" no service seguido de um insert — check-then-insert,
-- que dois envios simultâneos (duplo clique, retry de rede) atravessam juntos: duas candidaturas e,
-- na candidatura por currículo, duas extrações pagas. O índice fecha a corrida; o service trata a
-- violação (23505) como "você já se candidatou".
--
-- Parcial: ignora candidatos removidos (soft delete) e sem e-mail, que o fluxo público nunca cria
-- mas o seed/importações podem.
create unique index uq_candidates_campaign_email
  on candidates (campaign_id, email)
  where deleted_at is null and email is not null;

-- 2. Trilha de auditoria da avaliação por IA.
--
-- "A IA sugere, o humano decide" só é auditável se der para saber QUEM sugeriu: qual provedor, qual
-- modelo e qual versão do prompt produziram cada recomendação que estava na tela quando o RH
-- decidiu (candidate_decisions.assessment_id). Sem isto, trocar de modelo ou de prompt tornaria a
-- métrica "aderência às recomendações da IA" incomparável entre períodos.
--
-- Nuláveis: linhas anteriores a esta migration (seed, dados de teste inseridos à mão) não têm
-- origem conhecida, e inventar um valor seria pior que admitir que não se sabe.
alter table candidate_ai_assessments
  add column provider       text,
  add column model          text,
  add column prompt_version text;

-- Idempotência: uma avaliação por candidato, fase e versão de prompt. Duas requisições simultâneas
-- de avaliação não geram duas linhas (a segunda perde o INSERT ... ON CONFLICT DO NOTHING); mudar o
-- prompt_version é o que autoriza reavaliar. Parcial porque linhas sem versão (dados legados) não
-- devem colidir entre si.
create unique index uq_ai_assessment_per_phase_version
  on candidate_ai_assessments (candidate_id, phase_key, prompt_version)
  where prompt_version is not null;
