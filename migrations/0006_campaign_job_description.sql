-- Seções estruturadas da vaga — é o que o candidato anônimo lê na aba "Vaga" do link público
-- antes de decidir se se candidata (até agora a página só mostrava empresa, título e local).
-- Complementam `description` (0005), que segue sendo o resumo/abertura da vaga.
--
-- Nuláveis, como `description`: nenhuma é exigida na criação da campanha. A trava de qualidade do
-- link público olha SÓ `description` (ver SetPublicApplicationsEnabled em campaign/service.go) —
-- estas três nunca bloqueiam nada, é decisão de produto: nem toda vaga divulga benefícios, e
-- exigir isso só produziria texto de enchimento.
alter table campaigns
  add column responsibilities text,
  add column requirements     text,
  add column benefits         text;
