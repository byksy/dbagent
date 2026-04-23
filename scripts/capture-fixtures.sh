#!/usr/bin/env bash
#
# Captures EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) output for a handful
# of representative queries against the local docker-compose Postgres.
# Output lands in testdata/plans/real/.
#
# Requires: docker running, a Postgres container with the dbagent dev
# credentials (defaults match docker-compose.yml).

set -euo pipefail

CONTAINER="${DBAGENT_PG_CONTAINER:-docker_postgres}"
DB="${DBAGENT_PG_DB:-postgres}"
USER="${DBAGENT_PG_USER:-postgres}"

PSQL=(docker exec -i "$CONTAINER" psql -U "$USER" -d "$DB" -At)

mkdir -p testdata/plans/real

echo "→ setting up demo schema"
"${PSQL[@]}" <<'SQL'
CREATE TABLE IF NOT EXISTS customers (
    id int PRIMARY KEY,
    region text,
    name text
);
CREATE TABLE IF NOT EXISTS orders (
    id int PRIMARY KEY,
    customer_id int REFERENCES customers(id),
    status text,
    created_at timestamptz DEFAULT now(),
    amount numeric
);
CREATE INDEX IF NOT EXISTS customers_region_idx ON customers(region);

TRUNCATE customers, orders CASCADE;

INSERT INTO customers
SELECT i,
       CASE WHEN i % 3 = 0 THEN 'EU' WHEN i % 3 = 1 THEN 'US' ELSE 'APAC' END,
       'c' || i
FROM generate_series(1, 5000) i;

INSERT INTO orders
SELECT i,
       (i % 5000) + 1,
       CASE WHEN i % 4 = 0 THEN 'shipped' ELSE 'pending' END,
       now() - (random() * interval '90 days'),
       (random() * 1000)::numeric(10,2)
FROM generate_series(1, 50000) i;

ANALYZE;
SQL

capture() {
    local name="$1"
    local sql="$2"
    echo "→ capturing $name"
    "${PSQL[@]}" -c "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) $sql" \
        > "testdata/plans/real/${name}.json"
}

capture simple_seq_scan \
    "SELECT * FROM orders WHERE status = 'shipped' LIMIT 100"

capture hash_join_with_filter \
    "SELECT o.id, c.name FROM orders o JOIN customers c ON c.id = o.customer_id WHERE c.region = 'EU' AND o.status = 'shipped' ORDER BY o.created_at DESC LIMIT 42"

capture nested_loop_index \
    "SELECT * FROM orders o JOIN customers c ON c.id = o.customer_id WHERE o.id = 12345"

capture aggregate_group_by \
    "SELECT c.region, count(*), sum(o.amount) FROM customers c JOIN orders o ON o.customer_id = c.id WHERE o.status = 'shipped' GROUP BY c.region"

capture sort_small \
    "SELECT * FROM orders ORDER BY created_at DESC LIMIT 1000"

capture cte_with_join \
    "WITH recent AS (SELECT * FROM orders WHERE created_at > now() - interval '7 days') SELECT c.name, count(*) FROM recent r JOIN customers c ON c.id = r.customer_id GROUP BY c.name ORDER BY count(*) DESC LIMIT 10"

echo "✓ captured $(ls testdata/plans/real/*.json | wc -l | tr -d ' ') fixtures"
