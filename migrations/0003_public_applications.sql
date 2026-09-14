-- Candidatura pública por link de campanha — o recrutador liga um link compartilhável (reaproveita
-- o próprio uuid da campanha) que aceita candidaturas de qualquer pessoa, sem login.

-- Separado de `status`: pausar/encerrar a campanha já derruba o link (checado nas duas colunas
-- juntas na query pública), mas o recrutador também pode ligar/desligar só o link sem mexer no
-- status da campanha em si.
alter table campaigns
  add column accepts_public_applications boolean not null default false;

create index idx_campaigns_public_applications
  on campaigns (id) where accepts_public_applications and deleted_at is null;

-- Faltava — candidates.linkedin_url existe desde 0001, mas talents nunca teve. Sem isso, o dado se
-- perde no momento em que um candidato vira talento (reprovação qualificada ou, agora, candidatura
-- pública).
alter table talents
  add column linkedin_url text;

-- Nova origem de talento: a pessoa se candidatou sozinha, pelo link público — distinta de
-- reprovacao_qualificada (veio de uma vaga, reprovada com motivo que qualifica) e cadastro_manual
-- (recrutador que trouxe o perfil). Enum novo não pode ser usado em INSERT/UPDATE na MESMA
-- transação que o declara — não é problema aqui, é só a declaração; quem grava
-- 'candidatura_publica' é código de aplicação, bem depois.
alter type talent_origin add value 'candidatura_publica';
