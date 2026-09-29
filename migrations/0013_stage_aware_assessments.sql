-- Avaliação de IA "ciente da fase" — a IA já grava uma linha por (candidato, fase) desde a v1
-- (comentário em migrations/0001_init.sql, seção candidate_ai_assessments: "Fica histórico —
-- quando o candidato avança, uma nova assessment é criada"), mas até aqui o CONTEÚDO da avaliação
-- era o mesmo em toda fase (compara perfil × vaga, sem saber em qual etapa do funil está) e todo
-- caminho de leitura pegava "a mais recente, qualquer fase" em vez de "a desta fase" — ver
-- internal/domain/candidate/repository.go (latestAssessment, ListByCampaign, LatestAssessmentID),
-- corrigido no mesmo commit desta migration.
--
-- confidence é o único discriminador de estado, de propósito: em vez de um par (status,
-- confidence) redundante, 'insuficiente' É o quarto valor de confidence — quando a IA não tem
-- evidência para concluir nada de novo nesta fase, ela diz isso em vez de forçar uma nota. O modo
-- estrito do provedor (extraction.StrictObjectSchema) exige todo campo presente no JSON de saída
-- (sem "optional" de verdade), então match_pct/strengths/concerns continuam vindo preenchidos
-- mesmo quando confidence = 'insuficiente' — a UI é quem decide não mostrá-los como se fossem uma
-- conclusão real, olhando confidence, não o valor deles.
alter table candidate_ai_assessments
  add column confidence          text
    check (confidence in ('alta', 'media', 'baixa', 'insuficiente')),
  -- Insight curto e ESPECÍFICO DA FASE — distinto de justification (mais longa, genérica "por que
  -- essa nota"). Fica nulo nas fases sem foco definido ainda (recebidos, selecionados — ver
  -- stageFocus em internal/domain/candidate/assessment.go): nelas a avaliação continua sendo só a
  -- geral de sempre, sem uma segunda camada de texto que só repetiria matchNote.
  add column stage_insight        text,
  -- O que faltou para concluir com mais confiança nesta fase — preenchido nos dois casos (mesmo
  -- com confidence alta pode haver uma lacuna menor), texto livre curto por item.
  add column missing_information  text[] not null default '{}',
  -- Compara com a fase anterior avaliada deste candidato (não com a campanha inteira). Nulo/'' na
  -- primeira fase avaliada, que não tem o que comparar.
  add column comparison_flag      text
    check (comparison_flag in ('reforca_anterior', 'diverge_anterior', 'novo') or comparison_flag is null);

-- Linhas gravadas antes desta migration (prompt v1/v2) não têm confidence: são avaliação "cheia"
-- de sempre, sem o discriminador novo — tratadas pela UI como confidence implícito 'alta' (é o que
-- já eram, um match_pct e pronto), nunca como 'insuficiente'. Não fazer backfill: o valor real de
-- confidence daquela chamada nunca existiu, inventar um agora seria o mesmo tipo de inferência
-- artificial que esta migration existe para evitar.
