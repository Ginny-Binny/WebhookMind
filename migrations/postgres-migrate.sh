#!/bin/sh
# Idempotent Postgres migration runner used by docker compose's migrate-postgres service.
# Applies every /migrations/*.sql in alphabetical order on a fresh DB; no-ops on a DB
# that's already been migrated (detected by the presence of the `sources` table).

set -eu

already=$(psql -tAc "SELECT 1 FROM information_schema.tables WHERE table_name='sources' LIMIT 1" 2>/dev/null || true)
if [ -n "${already:-}" ]; then
  echo "postgres schema already present, skipping migrations"
  exit 0
fi

for f in /migrations/*.sql; do
  echo "applying $f"
  psql -v ON_ERROR_STOP=1 -f "$f"
done
echo "postgres migrations complete"
