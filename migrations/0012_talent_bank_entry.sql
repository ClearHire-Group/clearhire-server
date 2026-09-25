-- Separa "a pessoa existe na empresa" de "a pessoa está no Banco de Talentos".
--
-- Até aqui toda candidatura pelo link público criava um talento que já aparecia como parte do banco —
-- o banco virava depósito de currículo, que é exatamente o que a especificação proíbe ("nenhum caminho
-- de entrada automático"). O registro do talento continua nascendo na candidatura (é onde estão os
-- dados extraídos do currículo e o consentimento), mas só ENTRA no banco por um destes caminhos:
--   - reprovação com motivo que qualifica e envio ao banco confirmado pelo RH;
--   - aprovação (chegou a Selecionados) com consentimento dado;
--   - cadastro manual; importação.
-- bank_entered_at nulo = registro existe, mas não está no banco.
alter table talents add column bank_entered_at timestamptz;

comment on column talents.bank_entered_at is
  'Quando a pessoa entrou no Banco de Talentos. NULL = registro existe (ex.: candidatura pública em '
  'andamento) mas não faz parte do banco. Só a listagem/busca do banco filtra por isto.';

-- 1. Quem entrou por um caminho que já era de entrada no banco está no banco desde a criação.
update talents set bank_entered_at = created_at
where origin in ('reprovacao_qualificada', 'cadastro_manual', 'importacao') and bank_entered_at is null;

-- 2. Fusão de duplicatas: a mesma pessoa (mesmo e-mail na mesma empresa) virava dois talentos — um na
-- candidatura e outro na reprovação. Fica o registro com mais dado (mais skills; empate = o mais antigo),
-- herdando a entrada no banco do outro; as candidaturas passam a apontar para ele.
create temp table talent_merge on commit drop as
with ranked as (
  select t.id, t.company_id, t.email,
         row_number() over (
           partition by t.company_id, t.email
           order by (select count(*) from talent_skills s where s.talent_id = t.id) desc, t.created_at
         ) as rn
  from talents t
  where t.email is not null and t.deleted_at is null
)
select d.id as dup_id, k.id as keep_id
from ranked d
join ranked k on k.company_id = d.company_id and k.email = d.email and k.rn = 1
where d.rn > 1;

update talents k set
  origin          = case when k.bank_entered_at is null then x.entry_origin else k.origin end,
  bank_entered_at = coalesce(k.bank_entered_at, x.first_entry)
from (
  select m.keep_id,
         min(d.bank_entered_at) as first_entry,
         (array_agg(d.origin order by d.bank_entered_at))[1] as entry_origin
  from talent_merge m
  join talents d on d.id = m.dup_id
  where d.bank_entered_at is not null
  group by m.keep_id
) x
where k.id = x.keep_id;

update candidates c set talent_id = m.keep_id from talent_merge m where c.talent_id = m.dup_id;
delete from talents where id in (select dup_id from talent_merge);

-- 3. Aprovados com consentimento que já estão em Selecionados entram no banco (regra nova, aplicada
-- ao que já existe).
update talents t set
  origin = 'aprovacao',
  bank_entered_at = now()
where t.bank_entered_at is null
  and t.consent_state = 'consentido'
  and t.deleted_at is null
  and exists (
    select 1 from candidates c
    where c.talent_id = t.id and c.phase_key = 'selecionados' and c.deleted_at is null
  );

-- 4. Uma pessoa por e-mail por empresa, garantido pelo banco (citext: sem distinguir caixa). A
-- candidatura pública e a reprovação agora reaproveitam o registro existente em vez de criar outro.
create unique index uq_talents_company_email
  on talents (company_id, email)
  where deleted_at is null and email is not null;

create index idx_talents_in_bank
  on talents (company_id, bank_entered_at desc)
  where bank_entered_at is not null and deleted_at is null;
