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

mkdir -p testdata/plans/rules/{hot_node,row_misestimate,filter_removal_ratio,missing_index_on_filter,cte_cartesian_product,network_overhead,unused_index_hint,duplicate_index,composite_index_extension,table_bloat}/

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

# cte_cartesian_product — a correlated nested loop that rescans a CTE
# per outer row. The MATERIALIZED hint keeps PostgreSQL from inlining
# it and turning this rule into a no-op on newer planners.
capture cte_cartesian_product positive \
    "WITH recent AS MATERIALIZED (SELECT customer_id, sum(amount) s FROM rule_orders GROUP BY customer_id) SELECT r.customer_id, r.s FROM rule_orders o JOIN recent r ON r.customer_id = o.customer_id WHERE o.id < 200"
capture cte_cartesian_product negative \
    "WITH one AS (SELECT 1 AS x) SELECT * FROM one"

# network_overhead — SELECT * over 500k rows hits the >=10 MB tier.
capture network_overhead positive \
    "SELECT * FROM rule_orders"
capture network_overhead negative \
    "SELECT id FROM rule_orders LIMIT 10"

# unused_index_hint — create an index nobody queries, then run a scan
# on the parent table. The index's pg_stat_user_indexes.idx_scan stays
# at 0 so the rule fires once the schema is re-exported.
"${PSQL[@]}" <<'SQL' >/dev/null
DROP INDEX IF EXISTS rule_orders_stale_idx;
CREATE INDEX rule_orders_stale_idx ON rule_orders (created_at);
-- Reset stats so the index looks unused regardless of earlier runs.
SELECT pg_stat_reset_single_table_counters(oid) FROM pg_class WHERE relname = 'rule_orders';
SQL
capture unused_index_hint positive \
    "SELECT count(*) FROM rule_orders WHERE status = 'shipped'"
capture unused_index_hint negative \
    "SELECT count(*) FROM rule_orders WHERE id = 42"

# duplicate_index — two identical indexes on status, plan touches
# the table so the rule fires.
"${PSQL[@]}" <<'SQL' >/dev/null
DROP INDEX IF EXISTS rule_orders_status_idx;
DROP INDEX IF EXISTS rule_orders_status_copy_idx;
CREATE INDEX rule_orders_status_idx ON rule_orders (status);
CREATE INDEX rule_orders_status_copy_idx ON rule_orders (status);
SQL
capture duplicate_index positive \
    "SELECT count(*) FROM rule_orders WHERE status = 'shipped'"
capture duplicate_index negative \
    "SELECT count(*) FROM rule_orders WHERE id = 42"

# composite_index_extension — single-column index plus a query that
# filters on indexed column AND another column (trailing Filter).
"${PSQL[@]}" <<'SQL' >/dev/null
DROP INDEX IF EXISTS rule_orders_customer_idx;
CREATE INDEX rule_orders_customer_idx ON rule_orders (customer_id);
SQL
capture composite_index_extension positive \
    "SELECT * FROM rule_orders WHERE customer_id = 42 AND status = 'shipped'"
capture composite_index_extension negative \
    "SELECT * FROM rule_orders WHERE customer_id = 42"

# table_bloat — bloat a scratch table by inserting, deleting, and
# skipping VACUUM. A Seq Scan that reads many dead pages for few
# live rows trips the heuristic.
"${PSQL[@]}" <<'SQL' >/dev/null
DROP TABLE IF EXISTS rule_bloat;
CREATE TABLE rule_bloat (id int PRIMARY KEY, payload text);
INSERT INTO rule_bloat SELECT i, repeat('x', 200) FROM generate_series(1, 100000) i;
DELETE FROM rule_bloat WHERE id > 10;
ANALYZE rule_bloat;  -- update stats but NOT vacuum, so dead pages remain
SQL
capture table_bloat positive \
    "SELECT * FROM rule_bloat WHERE id < 1000"
capture table_bloat negative \
    "SELECT * FROM rule_orders WHERE id BETWEEN 1 AND 100"

echo "✓ captured $(ls testdata/plans/rules/*/real_*.json 2>/dev/null | wc -l | tr -d ' ') rule fixtures"
