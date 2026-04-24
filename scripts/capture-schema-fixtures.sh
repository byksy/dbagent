#!/usr/bin/env bash
#
# Regenerates testdata/schemas/{small,large}.json from the
# docker-compose Postgres. Committed alongside the captured plans so
# tests can exercise the schema-aware rules without requiring Docker
# at `go test` time.
#
# Shape:
#   small.json  — the Stage 2 schema (customers, orders with FK)
#   large.json  — Stage 2 + Stage 3 additions (rule_orders) so the
#                 export contains a wider variety of shapes
#
# Both captures are deterministic because the underlying seed scripts
# use `setseed(0.42)` and fixed timestamps. The only drift between
# runs is Meta.ExportedAt and the last_analyzed / size_bytes figures.

set -euo pipefail

CONFIG="testdata/configs/docker.yaml"

if [[ ! -x ./bin/dbagent ]]; then
    make build >/dev/null
fi

mkdir -p testdata/schemas

# --- small: Stage 2 schema only -------------------------------------------

CONTAINER="${DBAGENT_PG_CONTAINER:-docker_postgres}"
DB="${DBAGENT_PG_DB:-postgres}"
USER="${DBAGENT_PG_USER:-postgres}"
PSQL=(docker exec -i "$CONTAINER" psql -U "$USER" -d "$DB" -At)

echo "→ resetting schema to Stage 2 baseline"
"${PSQL[@]}" <<'SQL' >/dev/null
DROP TABLE IF EXISTS rule_orders CASCADE;
DROP TABLE IF EXISTS orders CASCADE;
DROP TABLE IF EXISTS customers CASCADE;
SQL

./scripts/capture-fixtures.sh >/dev/null

echo "→ exporting small schema"
./bin/dbagent --config "$CONFIG" schema export > testdata/schemas/small.json

# --- large: add Stage 3 rule_orders on top --------------------------------

echo "→ adding Stage 3 rule_orders"
./scripts/capture-rules-fixtures.sh >/dev/null

echo "→ exporting large schema"
./bin/dbagent --config "$CONFIG" schema export > testdata/schemas/large.json

echo "✓ captured $(ls testdata/schemas/*.json | wc -l | tr -d ' ') schema fixtures"
