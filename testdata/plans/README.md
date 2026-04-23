# Plan fixtures

Two subdirectories of `EXPLAIN (FORMAT JSON)` output used by the
`internal/plan` parser and the `internal/cli` renderers.

## `real/`

Captured from the local `docker-compose` Postgres by
`scripts/capture-fixtures.sh` (`make fixtures`). These reflect actual
Postgres 17 output shapes against a seeded schema (`customers` +
`orders`).

The script uses `SELECT setseed(0.42)` and a fixed reference timestamp,
so the underlying row data is deterministic. Plan *shape* (node types,
relations, filters, indexes) therefore stays stable across runs. Plan
*measurements* — `Actual Total Time`, `Planning Time`, `Total Cost`,
and row-estimate figures from `ANALYZE`'s internal sampling — will
vary slightly on each capture, so `git diff` after `make fixtures` is
expected to show some numeric churn even without any code changes.

If you change the schema or the queries in `capture-fixtures.sh`,
re-run `make fixtures` and commit the updated JSON. After a
regeneration, also re-run
`go test ./internal/cli -run Golden -update` to refresh the golden
renderer outputs.

## `synthetic/`

Hand-written JSON files that pin specific parser behaviors the real
fixtures may not exercise (for example, the PostgreSQL planner often
inlines CTEs into scans, so a real capture may never produce an
`InitPlan`/`CTE Scan` shape). Each file is intentionally minimal —
just enough node shape to trigger the parser branch under test.

Do not regenerate these; edit them by hand.

## `../golden/`

Expected renderer output. Regenerate with
`go test ./internal/cli -run Golden -update` after deliberate changes
to the renderer, and commit the updated files.
