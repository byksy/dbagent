#!/usr/bin/env bash
#
# Generates rule-specific EXPLAIN fixtures against docker_postgres.
# Outputs land under testdata/plans/rules/<rule>/real_<variant>.json
# so the committed synthetic fixtures (used by unit tests) are not
# overwritten — these real captures are realistic examples, not test
# inputs, since their timing/cost numbers shift between runs.
#
# Re-run this after changing the queries or demo data shape.

set -euo pipefail

CONTAINER="${DBAGENT_PG_CONTAINER:-docker_postgres}"
DB="${DBAGENT_PG_DB:-postgres}"
USER="${DBAGENT_PG_USER:-postgres}"

PSQL=(docker exec -i "$CONTAINER" psql -U "$USER" -d "$DB" -At)

mkdir -p testdata/plans/rules/{hot_node,row_misestimate,filter_removal_ratio,missing_index_on_filter}/

echo "→ setting up rule-fixtures schema (deterministic seed)"
"${PSQL[@]}" <<'SQL'
DROP TABLE IF EXISTS rule_orders CASCADE;
CREATE TABLE rule_orders (
    id int PRIMARY KEY,
    customer_id int,
    status text,
    created_at timestamptz,
    amount numeric
);

SELECT setseed(0.42);

INSERT INTO rule_orders
SELECT i,
       (i % 10000) + 1,
       CASE WHEN i % 100 = 0 THEN 'shipped' ELSE 'pending' END,
       TIMESTAMP '2026-03-01 00:00:00' - (random() * interval '365 days'),
       (random() * 1000)::numeric(10,2)
FROM generate_series(1, 500000) i;

ANALYZE rule_orders;
SQL

capture() {
    local rule="$1" variant="$2" sql="$3"
    echo "→ capturing $rule/real_$variant"
    "${PSQL[@]}" -c "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) $sql" \
        > "testdata/plans/rules/${rule}/real_${variant}.json"
}

# hot_node — a forced full scan over all 500k rows with a CPU-heavy
# predicate so total time clears the 10ms noise gate on any machine.
# The md5 input avoids timestamptz::text, whose rendering depends on
# the session's TimeZone/DateStyle GUCs, to keep captures byte-stable
# across environments.
capture hot_node positive \
    "SELECT count(*) FROM rule_orders WHERE amount > 500 AND md5(status || customer_id::text) LIKE 'a%'"
capture hot_node negative \
    "SELECT * FROM rule_orders WHERE id = 42"

# filter_removal_ratio — 1% of rows are 'shipped', so filtering by that
# status discards the other 99%.
capture filter_removal_ratio positive \
    "SELECT * FROM rule_orders WHERE status = 'shipped'"
capture filter_removal_ratio negative \
    "SELECT * FROM rule_orders WHERE id BETWEEN 1 AND 100"

# missing_index_on_filter — same shape as filter_removal_ratio positive,
# no matching index on status.
capture missing_index_on_filter positive \
    "SELECT * FROM rule_orders WHERE status = 'shipped'"
capture missing_index_on_filter negative \
    "SELECT * FROM rule_orders WHERE id = 42"

# row_misestimate — insert skewed data WITHOUT re-ANALYZE to force a
# stale planner estimate.
echo "→ inserting skewed data without ANALYZE (for row_misestimate)"
"${PSQL[@]}" <<'SQL'
INSERT INTO rule_orders
SELECT 500000 + i, 1, 'shipped', TIMESTAMP '2026-03-01 00:00:00', 999
FROM generate_series(1, 100000) i;
-- deliberately skip ANALYZE
SQL
capture row_misestimate positive_100x \
    "SELECT * FROM rule_orders WHERE status = 'shipped'"

echo "✓ captured $(ls testdata/plans/rules/*/real_*.json 2>/dev/null | wc -l | tr -d ' ') rule fixtures"
