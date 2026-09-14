-- Popula candidatos de teste numa campanha já existente, pra validar Get/Listar/Decidir sem
-- precisar de um fluxo real de ingestão (que ainda não existe — ver CLAUDE.md/conversa que criou
-- este script). NÃO é uma migration: é dado de teste, roda sob demanda, quantas vezes quiser.
--
-- Uso (não chame psql direto neste arquivo — o placeholder __CAMPAIGN_ID__ precisa ser
-- substituído antes; o wrapper faz isso):
--   ./scripts/seed_candidates.sh <uuid-da-campanha>
--
-- Cada bloco só insere se a fase estiver de fato configurada nesta campanha (campaign_phases) —
-- uma campanha pode ter sido criada sem fit/tecnica/entrevista (só Recebidos + Selecionados são
-- fixos), então inserir direto quebraria a FK (candidates.campaign_id, phase_key) ->
-- campaign_phases.
--
-- (Nota técnica: psql não faz substituição de variável -v dentro de bloco $$ ... $$ — por isso o
-- wrapper usa sed pra trocar __CAMPAIGN_ID__ antes de mandar pro psql, em vez de :campaign_id.)

\set ON_ERROR_STOP on

do $$
declare
  v_campaign_id uuid := '__CAMPAIGN_ID__';
  v_company_id uuid;
  v_cand_id uuid;
  v_assessment_id uuid;
  v_skill_backend uuid;
  v_skill_python uuid;
begin
  select company_id into v_company_id from campaigns where id = v_campaign_id;
  if v_company_id is null then
    raise exception 'campanha % não encontrada', v_campaign_id;
  end if;

  -- Skills de apoio (idempotente — reaproveita se já existir de uma rodada anterior do script).
  insert into skills (canonical_term, category) values ('Python', 'Dados')
    on conflict (canonical_term) do nothing;
  insert into skills (canonical_term, category) values ('Arquitetura de Sistemas', 'Backend')
    on conflict (canonical_term) do nothing;
  select id into v_skill_python from skills where canonical_term = 'Python';
  select id into v_skill_backend from skills where canonical_term = 'Arquitetura de Sistemas';

  -- Recebidos (fixa em toda campanha) — 2 candidatos aguardando a primeira triagem.
  insert into candidates (company_id, campaign_id, name, email, city, state, years_experience, phase_key, status)
  values
    (v_company_id, v_campaign_id, 'André Cavalcanti', 'andre.cavalcanti@exemplo.dev', 'Salvador', 'BA', 6, 'recebidos', 'aguardando_triagem_ia'),
    (v_company_id, v_campaign_id, 'Priscila Homem', 'priscila.homem@exemplo.dev', 'Campinas', 'SP', 3, 'recebidos', 'aguardando_triagem_ia');

  -- Fit Cultural — só se a campanha tiver esse módulo configurado.
  if exists (select 1 from campaign_phases where campaign_id = v_campaign_id and phase_key = 'fit') then
    insert into candidates (company_id, campaign_id, name, email, city, state, years_experience, phase_key, status, summary)
    values (v_company_id, v_campaign_id, 'Camila Duarte', 'camila.duarte@exemplo.dev', 'Porto Alegre', 'RS', 4, 'fit', 'em_analise',
      'Perfil colaborativo, forte comunicação assíncrona, já trabalhou em squads distribuídos.')
    returning id into v_cand_id;

    insert into candidate_ai_assessments (candidate_id, phase_key, match_pct, match_label, match_note, justification)
    values (v_cand_id, 'fit', 79, 'Bom fit', 'Alinhamento forte com valores de autonomia',
      'Respostas do formulário cultural indicam alta aderência a trabalho assíncrono e feedback direto.')
    returning id into v_assessment_id;
    insert into candidate_ai_assessment_points (assessment_id, kind, text, position) values
      (v_assessment_id, 'strength', 'Comunicação assíncrona forte', 1),
      (v_assessment_id, 'concern', 'Pouca experiência em squads grandes (>8 pessoas)', 1);
  end if;

  -- Triagem Técnica — idem.
  if exists (select 1 from campaign_phases where campaign_id = v_campaign_id and phase_key = 'tecnica') then
    insert into candidates (company_id, campaign_id, name, email, city, state, years_experience, phase_key, status, summary)
    values (v_company_id, v_campaign_id, 'Lucas Peixoto', 'lucas.peixoto@exemplo.dev', 'São Paulo', 'SP', 9, 'tecnica', 'em_analise',
      'Nove anos de backend, forte em arquitetura distribuída e Python.')
    returning id into v_cand_id;

    insert into candidate_experience_entries (candidate_id, role, company, period_label, description, position) values
      (v_cand_id, 'Engenheiro de Software Sênior', 'Empresa Anterior Ltda', '2019 — atual',
       'Liderou a migração de monólito para microsserviços, reduzindo tempo de deploy em 60%.', 1);

    insert into candidate_skills (candidate_id, skill_id, level, years_experience) values
      (v_cand_id, v_skill_python, 'avançado', 9),
      (v_cand_id, v_skill_backend, 'avançado', 6);

    insert into candidate_ai_assessments (candidate_id, phase_key, match_pct, match_label, match_note, justification)
    values (v_cand_id, 'tecnica', 76, 'Bom match técnico', 'Forte em arquitetura, revisar profundidade em testes automatizados',
      'Currículo e desafio técnico mostram domínio sólido de Python e design de sistemas distribuídos.')
    returning id into v_assessment_id;
    insert into candidate_ai_assessment_points (assessment_id, kind, text, position) values
      (v_assessment_id, 'strength', 'Arquitetura de sistemas distribuídos', 1),
      (v_assessment_id, 'strength', 'Python avançado', 2),
      (v_assessment_id, 'concern', 'Pouco detalhe sobre testes automatizados no currículo', 1);
  end if;

  -- Entrevista Estruturada — candidato "aguardando decisão" (o caso mais útil pra testar avançar/reprovar).
  if exists (select 1 from campaign_phases where campaign_id = v_campaign_id and phase_key = 'entrevista') then
    insert into candidates (company_id, campaign_id, name, email, city, state, years_experience, phase_key, status, summary)
    values (v_company_id, v_campaign_id, 'Marina Albuquerque', 'marina.albuquerque@exemplo.dev', 'São Paulo', 'SP', 8, 'entrevista', 'aguardando_decisao',
      'Entrevista estruturada concluída — forte em liderança técnica e comunicação com stakeholders.')
    returning id into v_cand_id;

    insert into candidate_ai_assessments (candidate_id, phase_key, match_pct, match_label, match_note, justification)
    values (v_cand_id, 'entrevista', 94, 'Excelente match', 'Um dos perfis mais fortes já avaliados nesta campanha',
      'Entrevista estruturada confirmou as competências técnicas e trouxe evidência forte de liderança.')
    returning id into v_assessment_id;
    insert into candidate_ai_assessment_points (assessment_id, kind, text, position) values
      (v_assessment_id, 'strength', 'Liderança técnica comprovada', 1),
      (v_assessment_id, 'strength', 'Comunicação clara com stakeholders não-técnicos', 2);
  end if;

  -- Selecionados (fixa) — proposta em elaboração.
  insert into candidates (company_id, campaign_id, name, email, city, state, years_experience, phase_key, status)
  values (v_company_id, v_campaign_id, 'Diego Salgado', 'diego.salgado@exemplo.dev', 'Rio de Janeiro', 'RJ', 10, 'selecionados', 'proposta');

  raise notice 'Seed concluído para a campanha %', v_campaign_id;
end $$;
