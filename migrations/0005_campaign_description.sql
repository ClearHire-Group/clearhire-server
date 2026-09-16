-- Descrição livre da vaga — campo que a tela "Configurações da Campanha" sempre prometeu editar
-- (texto "Editar título, descrição e as fases do funil"), mas não existia em nenhuma camada até
-- agora. Nula até o recrutador preencher via PATCH /campaigns/:id — não é exigida na criação.
alter table campaigns
  add column description text;
