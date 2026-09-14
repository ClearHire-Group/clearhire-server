-- Popula rejection_reasons — a Tabela 1 do documento de especificação, já espelhada em
-- clearhire-app/src/app/core/models.ts (REJECTION_REASONS). 0001_init.sql criou a tabela mas nunca
-- inseriu as linhas; candidates.rejection_reason_key é FK pra cá, então decidir uma reprovação
-- travava (violação de FK) até esta migration rodar.
insert into rejection_reasons (key, label, goes_to_bank, effect_label, position) values
  ('perdeu_outro_candidato', 'Perdeu para outro candidato', true,
    'Perfil de maior valor do banco. Passou por todo o funil e foi aprovado tecnicamente.', 1),
  ('senioridade_acima', 'Senioridade acima da vaga', true,
    'Sinaliza para vagas de senioridade superior.', 2),
  ('faltou_skill', 'Faltou skill específica', true,
    'Registrar qual skill faltou. Revisitar em ~12 meses.', 3),
  ('pretensao_acima_budget', 'Pretensão acima do budget', true,
    'Registrar faixa. Pode caber em vaga com budget maior.', 4),
  ('timing_indisponibilidade', 'Timing / indisponibilidade', true,
    'Registrar quando volta a estar disponível.', 5),
  ('reprovacao_tecnica', 'Reprovação técnica de fundo', false,
    'Não reaproveitar. Poluiria o banco.', 6),
  ('fit_cultural_incompativel', 'Fit cultural incompatível', false,
    'Não reaproveitar.', 7)
on conflict (key) do nothing;
