#!/usr/bin/env bash
# Popula candidatos de teste numa campanha já existente — uso:
#   ./scripts/seed_candidates.sh <uuid-da-campanha>
# Lê DATABASE_URL do .env local se existir; senão usa o Postgres de dev padrão (.devdb, porta
# 55555, ver CLAUDE.md do servidor).
set -euo pipefail

CAMPAIGN_ID="${1:?uso: scripts/seed_candidates.sh <uuid-da-campanha>}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -f "$SCRIPT_DIR/../.env" ]; then
  # shellcheck disable=SC1091
  DATABASE_URL="$(grep -E '^DATABASE_URL=' "$SCRIPT_DIR/../.env" | cut -d '=' -f2-)"
fi
DATABASE_URL="${DATABASE_URL:-postgres://clearhire@localhost:55555/clearhire_dev?sslmode=disable&host=/tmp}"

sed "s/__CAMPAIGN_ID__/${CAMPAIGN_ID}/g" "$SCRIPT_DIR/seed_candidates.sql" | psql "$DATABASE_URL"
