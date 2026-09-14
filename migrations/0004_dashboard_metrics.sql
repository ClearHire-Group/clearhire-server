-- Meta de contratações por período, exibida no card "Contratações no período" do Dashboard
-- (`hiresGoal` em core/models.ts). Diferente das outras métricas do dashboard, isto NÃO é
-- derivável de campanhas/candidatos — é uma configuração da empresa (quantas contratações ela
-- pretende fechar no período corrente). Vira coluna, não número fixo no código: a mesma regra do
-- topo de 0001_init.sql ("nunca hardcodadas soltas") vale aqui, só que pro dado que não é uma
-- agregação. Default 10 é só um chute inicial razoável; não existe tela de edição ainda — quando
-- houver, é aqui que ela escreve.
alter table companies
  add column hiring_goal_per_period int not null default 10 check (hiring_goal_per_period >= 0);
