#!/usr/bin/env bash
# Aplica, em ordem, as migrations de migrations/ que ainda não constam em schema_migrations.
#
# Cada arquivo roda numa transação própria (--single-transaction), junto com o registro dele em
# schema_migrations: ou a migration inteira entra e fica registrada, ou nada muda. Sem a transação,
# 0012 quebraria — o `create temp table ... on commit drop` dela sumiria no autocommit do próprio
# statement, antes de ser usado.
#
# Uso:
#   DATABASE_URL=... scripts/migrate.sh              aplica o que falta
#   DATABASE_URL=... scripts/migrate.sh --baseline   só REGISTRA as pendentes como aplicadas, sem
#                                                    rodá-las (banco criado por scripts/supabase_schema.sql)
set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL não definida}"
cd "$(dirname "$0")/.."

run_psql() { psql "$DATABASE_URL" -X -q -v ON_ERROR_STOP=1 "$@"; }

run_psql -c "create table if not exists schema_migrations (
  version    text primary key,
  applied_at timestamptz not null default now()
)"

applied=$(run_psql -At -c "select version from schema_migrations")

for file in migrations/*.sql; do
  version=$(basename "$file" .sql)
  if grep -qxF "$version" <<<"$applied"; then
    continue
  fi

  if [[ "${1:-}" == "--baseline" ]]; then
    echo "registrando $version (baseline, sem executar)"
    run_psql -c "insert into schema_migrations (version) values ('$version')"
    continue
  fi

  echo "aplicando $version"
  run_psql --single-transaction \
    -f "$file" \
    -c "insert into schema_migrations (version) values ('$version')"
done

# No Supabase o schema public é exposto pela Data API (PostgREST) a quem tiver a anon key. RLS
# ligado sem policy nega tudo a esses roles; o backend conecta como dono das tabelas e não é
# afetado. Roda a cada execução para que tabela nova de migration futura nunca nasça exposta.
run_psql -c "do \$\$
declare t record;
begin
  for t in select tablename from pg_tables where schemaname = 'public' and not rowsecurity loop
    execute format('alter table public.%I enable row level security', t.tablename);
  end loop;
end \$\$"

echo "migrations em dia"
