# Rules

`dbagent analyze` runs seventeen rules against every parsed plan. Each rule is either **diagnostic** (describes what's wrong) or **prescriptive** (suggests a specific fix). This page lists each rule's conditions, severity tiers, and a sample finding.

> **Extraction caveat.** Rules that parse filter, index-cond, or sort-key expressions use regex-based column extraction. This handles common cases (simple comparisons, AND/OR chains, IN clauses) but may miss columns inside complex expressions (nested function calls, CASE/WHEN, arbitrary expression-based conditions). See [`decisions.md`](decisions.md) for why we defer a full SQL parser.

## Contents

- [hot_node](#hot_node) — diagnostic
- [row_misestimate](#row_misestimate) — diagnostic (schema-aware)
- [filter_removal_ratio](#filter_removal_ratio) — diagnostic
- [missing_index_on_filter](#missing_index_on_filter) — prescriptive (schema-aware)
- [bitmap_and_composite](#bitmap_and_composite) — prescriptive (schema-aware)
- [sort_spilled](#sort_spilled) — prescriptive
- [planning_vs_execution](#planning_vs_execution) — prescriptive
- [worker_shortage](#worker_shortage) — diagnostic
- [fk_missing_index](#fk_missing_index) — prescriptive (schema-aware, new in v0.4)
- [cte_cartesian_product](#cte_cartesian_product) — prescriptive (new in v0.5)
- [network_overhead](#network_overhead) — diagnostic (new in v0.5)
- [redundant_aggregation](#redundant_aggregation) — prescriptive (new in v0.5)
- [memoize_opportunity](#memoize_opportunity) — prescriptive (new in v0.5)
- [unused_index_hint](#unused_index_hint) — diagnostic (schema-aware, new in v0.5)
- [duplicate_index](#duplicate_index) — prescriptive (schema-aware, new in v0.5)
- [composite_index_extension](#composite_index_extension) — prescriptive (schema-aware, new in v0.5)
- [table_bloat](#table_bloat) — prescriptive (new in v0.5)

Rules that compute row ratios always skip `NeverExecuted` nodes, so a never-taken branch cannot inflate a rule's severity.

---

## hot_node

**Category:** Diagnostic · **Severity:** Info / Warning / Critical

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags nodes whose exclusive execution time dominates the query.

**Conditions:**

- `ExclusiveTimeMs / TotalTimeMs >= 30%` → Info
- `>= 50%` → Warning
- `>= 90%` → Critical

**Example finding:**

> [4] Seq Scan on orders — this node accounts for 54% of total query time (82.4ms of 152.3ms).

**What to do:**

Hot nodes are where optimization effort pays off. Check whether the node has a corresponding `missing_index_on_filter` or `row_misestimate` finding for concrete next steps.

---

## row_misestimate

**Category:** Diagnostic · **Severity:** Info / Warning / Critical

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags nodes where the planner's per-loop row estimate diverges significantly from actual.

**Conditions** (factor = `max(plan_rows/actual, actual/plan_rows)` comparing per-loop figures):

- `factor >= 10` → Info
- `factor >= 100` → Warning
- `factor >= 1000` → Critical

For base-relation scans (Seq Scan, Index Scan, etc.) the finding includes a `Suggested: ANALYZE <relation>;` line. Non-scan nodes (joins, aggregates) do not receive a suggestion — the cause is usually upstream.

**Example finding:**

> [4] Seq Scan on orders — planner estimated 5,000 rows, actual is 48,921 (9.8× under). Suggested: `ANALYZE orders;`

**Schema-aware behavior (since v0.4):**
- If a schema is loaded (live fetch or `--schema`), the finding also carries `last_analyzed` and `analyze_age_hours` on the Evidence map.
- When the table's last `ANALYZE` is more than 7 days old, the rule bumps severity by one tier (Info → Warning → Critical) and appends "Last ANALYZE was N days ago." to the message.

---

## filter_removal_ratio

**Category:** Diagnostic · **Severity:** Info / Warning / Critical

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags scans that discard the bulk of the rows they read.

**Conditions** (only applies to Seq Scan, Index Scan, Index Only Scan, Bitmap Heap Scan):

- `ratio >= 50% AND removed >= 100` → Info
- `ratio >= 80% AND removed >= 100` → Warning
- `ratio >= 95% AND removed >= 1000` → Critical

**Example finding:**

> [4] Seq Scan on orders — 99% of rows read are discarded by filter (495,000 rows removed, 5,000 kept).

Look for a companion `missing_index_on_filter` finding on the same node for the prescriptive side.

---

## missing_index_on_filter

**Category:** Prescriptive · **Severity:** Warning / Critical

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Suggests a `CREATE INDEX` when a scan with a `Filter` (not Index Cond) discards most of what it scanned at meaningful volume.

**Conditions** (all must hold):

- Node type is Seq Scan, Bitmap Heap Scan, Index Scan, or Index Only Scan
- A `Filter` clause is present
- `5 * rows_kept < rows_removed` (filter removes > 80% of the scan)
- `loops * rows_removed >= 100` (enough volume to matter)

Severity escalates to Critical when `loops * rows_removed > 10_000`.

The `Suggested` line is only emitted when the filter predicate can be confidently parsed into one or more column names. If the filter uses complex functions or expressions we can't parse, the finding still appears (as a diagnostic) but without a Suggested. This is deliberate — a `CREATE INDEX` targeting the wrong column costs more user trust than no suggestion at all.

**Example finding:**

> [4] Seq Scan on orders — filter removes 99% of rows scanned (495,000 rows). Consider an index to push the predicate down. Suggested: `CREATE INDEX ON orders (status);`

**Schema-aware behavior (since v0.4):**
- If an index already covers the filter columns, this rule does not fire.
- If an index exists on a prefix of the filter columns, the rule suggests extending the existing index (DROP + recreate with extra columns) rather than creating a duplicate.
- If no schema is available (offline mode without `--schema`), the rule emits the plain `CREATE INDEX` suggestion with a note that coverage wasn't verified.

---

## bitmap_and_composite

**Category:** Prescriptive · **Severity:** Warning

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags `BitmapAnd` nodes built from two or more separate `Bitmap Index Scan` children on the same relation. A composite index covering all the conditions avoids the `BitmapAnd` step.

**Conditions:**

- Node type is `BitmapAnd`
- ≥ 2 `Bitmap Index Scan` children
- Parent scan names a relation

Columns in the proposed index are ordered by selectivity (fewest matching rows first), which is a reasonable heuristic but not guaranteed optimal.

**Example finding:**

> [3] BitmapAnd — planner combined 2 separate bitmap index scans. A composite index on (customer_id, status) would avoid the BitmapAnd step. Suggested: `CREATE INDEX ON orders (customer_id, status);`

**Schema-aware behavior (since v0.4):**
- If a composite index already covers the proposed columns, this rule does not fire (the planner chose BitmapAnd anyway, usually because of selectivity estimates).

---

## sort_spilled

**Category:** Prescriptive · **Severity:** Warning / Critical

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags `Sort` or `Incremental Sort` nodes that overflowed `work_mem` and spilled to disk.

**Conditions:**

- Node type is `Sort` or `Incremental Sort`
- `Sort Space Type == "Disk"`
- Severity is Critical when `Sort Space Used > 1,000,000 kB` (~1 GB)

The suggested `work_mem` value is 1.2× the spilled size, rounded up to the next power-of-2 megabyte, capped at 2 GB.

**Example finding:**

> [2] Sort — sort spilled to disk (524,288kB). Increasing work_mem would keep it in memory. Suggested: `SET LOCAL work_mem = '1GB';`

---

## planning_vs_execution

**Category:** Prescriptive · **Severity:** Info

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags queries where planning takes longer than execution. For frequently-run short queries, `PREPARE`/`EXECUTE` amortises the planning cost across calls.

**Conditions:**

- `PlanningTimeMs > ExecutionTimeMs`
- `ExecutionTimeMs < 10ms` (only matters for fast queries)

The finding has `NodeID = 0` (plan-level) and no Suggested line, since the remediation is a usage pattern change, not a single SQL statement.

**Example finding:**

> Plan — planning (12.3ms) exceeds execution (2.8ms). Consider PREPARE/EXECUTE for frequent, fast queries.

---

## worker_shortage

**Category:** Diagnostic · **Severity:** Info / Warning

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags `Gather` / `Gather Merge` nodes that received fewer parallel workers than the planner intended.

**Conditions:**

- Node type is `Gather` or `Gather Merge`
- `WorkersLaunched < WorkersPlanned`

Severity:

- Shortfall of 1 worker → Info
- Shortfall of ≥ 2 workers → Warning

**Example finding:**

> [1] Gather — only 2 of 4 planned workers were launched. Consider raising max_parallel_workers.

The remediation is a configuration change, not a SQL statement, so no Suggested is emitted. Check `max_parallel_workers_per_gather`, `max_parallel_workers`, and whether concurrent load is exhausting the pool.

---

## fk_missing_index

**Category:** Prescriptive · **Severity:** Warning

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags foreign-key columns that lack a supporting leading-column btree index. Unindexed FK columns slow down joins that probe the FK side and make cascading DELETEs/UPDATEs do sequential scans on the referencing table.

This rule needs a loaded schema (live fetch or `--schema <file>`). Offline analysis without a schema skips the rule entirely.

**Conditions:**

- `ctx.Schema` is populated
- The schema has at least one foreign key whose columns are NOT covered by a leading-column btree index (partial indexes and non-btree methods don't count)
- The plan contains a scan on the affected referencing table (so the finding stays anchored to the query being analyzed)

**Example finding:**

> [1] Seq Scan on payments — Table public.payments has a foreign key "payments_order_fkey" on column(s) (order_id) without a supporting index. Joins and cascading operations on this column will be slow.
> Suggested: `CREATE INDEX ON public.payments (order_id);`

**What to do:** create the suggested index. If the FK is a composite, the index must cover the columns in the same order.

---

## cte_cartesian_product

**Category:** Prescriptive · **Severity:** Warning / Critical · *New in v0.5*

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags `CTE Scan` nodes that are rescanned many times and accumulate significant row work. PostgreSQL ≥ 12 often inlines non-recursive CTEs, but the inlining is conservative — a CTE used as the inner side of a nested loop still reruns per outer row. Converting such a CTE to a JOIN or subquery typically avoids the repeated work.

**Conditions:** Node is `CTE Scan` · `Loops ≥ 10` · `Loops × (ActualRows + RowsRemovedByFilter) ≥ 10,000`.

**Severity:** Warning below 100,000 cumulative rows, Critical at or above.

**Example finding:**

> [4] CTE Scan on r — CTE "recent_orders" was scanned 50 times, processing 10,500 rows cumulatively. Converting to a JOIN or subquery typically avoids repeated work.

**What to do:** rewrite the CTE as a JOIN or subquery, or wrap it with `AS MATERIALIZED` only when the repeat work is actually cheaper than materialising once.

---

## network_overhead

**Category:** Diagnostic · **Severity:** Info / Warning / Critical · *New in v0.5*

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags queries that return a lot of data to the client. The rule only looks at the plan root (the shape the client actually sees).

**Conditions:** Root emits `ActualRowsTotal × PlanWidth ≥ 10 MB`.

**Severity tiers:** 10–100 MB → Info, 100 MB–1 GB → Warning, ≥ 1 GB → Critical.

**Example finding:**

> (plan-level) — Query returns approximately 122.1 MB to the client (500,000 rows × 256 bytes). Consider LIMIT, column projection, or server-side aggregation.

**What to do:** add a `LIMIT`, project specific columns instead of `SELECT *`, or push aggregation / pagination to the server.

---

## redundant_aggregation

**Category:** Prescriptive · **Severity:** Info · *New in v0.5*

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags `Aggregate` nodes that use the `Hashed` strategy immediately above a `Sort` whose keys already group the input. In those cases a `GroupAggregate` would skip the hash table entirely.

**Conditions:** Aggregate with `Strategy == "Hashed"` · immediate non-InitPlan child is `Sort` · Sort's leading keys cover the aggregate's `GroupKey`.

**Example finding:**

> [1] Aggregate — HashAggregate above a Sort that could feed GroupAggregate directly. Consider enforcing GroupAggregate via query shape or enable_hashagg settings.

**What to do:** check `enable_hashagg` / `work_mem` tuning, or reshape the query so the planner sees the sorted input.

---

## memoize_opportunity

**Category:** Prescriptive · **Severity:** Info · *New in v0.5*

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags `Nested Loop` joins whose inner side re-probes on repeated keys (many loops, near-zero rows per loop) without a `Memoize` node. PostgreSQL 14 added `Memoize`; the planner picks it based on statistics, so stale stats can make it miss.

**Conditions:** Nested Loop · inner child has `Loops ≥ 100` · inner `ActualRows ≤ 1` on average · no `Memoize` node between the join and the inner scan · plan server version ≥ 14 (unknown version is treated as safe to fire).

**Example finding:**

> [1] Nested Loop — Nested Loop with 5,000 inner iterations averaging 0.0 rows each. A Memoize node could cache repeated lookups; check ANALYZE freshness and enable_memoize. Suggested: `ANALYZE trips;`

**What to do:** run `ANALYZE` on the inner table, or enable `enable_memoize` if it was turned off.

---

## unused_index_hint

**Category:** Diagnostic · **Severity:** Info · *New in v0.5*

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Informational alert: one or more non-primary, non-unique indexes on a table the query touches show zero scans in `pg_stat_user_indexes`. The rule intentionally **does not** suggest `DROP INDEX`; indexes may back rare workflows or constraints, and the call is the operator's.

**Conditions:** schema is loaded · at least one index on the touched table has `Scans > 0` (protects against legacy schema exports where the field is uniformly zero) · the candidate index has `Scans == 0` and is neither primary nor unique.

**Example finding:**

> [1] Seq Scan on orders — Table public.orders has 1 index(es) with zero scans recorded since last stats reset: orders_stale_idx. Review whether they're still needed. dbagent does not generate DROP INDEX statements automatically.

**What to do:** confirm the index isn't maintained for rare workflows or constraints; if truly unused, drop it by hand.

---

## duplicate_index

**Category:** Prescriptive · **Severity:** Warning · *New in v0.5*

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags pairs of indexes on the same table whose column lists are identical (order-sensitive — `(a, b)` and `(b, a)` are not duplicates).

**Conditions:** schema loaded · two or more non-partial indexes on the touched table share an ordered column list.

**Drop candidate selection:** the rule proposes dropping the non-primary, non-unique index with the lexicographically later name. When every candidate in the group is constraint-backed, the finding still appears but the `Suggested` line is omitted — dropping a primary key or unique index would break the constraint.

**Example finding:**

> [1] Seq Scan on orders — Table public.orders has duplicate indexes on (customer_id, status): orders_cust_status_idx and orders_duplicate_cust_status_idx. Consider dropping one.
> Suggested: `DROP INDEX public.orders_duplicate_cust_status_idx;`

**What to do:** drop the proposed index after confirming no application or monitoring pins it by name.

---

## composite_index_extension

**Category:** Prescriptive · **Severity:** Warning · *New in v0.5*

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags `Index Scan` / `Index Only Scan` nodes that still carry a trailing `Filter` — the index supplied the index condition, but additional predicates re-check every row afterwards. Extending the index to cover the filter columns often removes the extra work. Complements `missing_index_on_filter`, which targets Seq/Bitmap Heap scans without a usable index.

**Conditions:** schema loaded · node is an Index Scan or Index Only Scan with a non-empty `Filter` · the referenced index is non-primary, non-unique, non-partial, btree.

**Example finding:**

> [1] Index Scan on orders — Index "orders_customer_idx" covers the index condition but not the trailing filter. Extending to (customer_id, status) would avoid the additional work.

**What to do:** replace the index with the extended version in a controlled deploy window (or use `CREATE INDEX CONCURRENTLY` in a two-step migration).

---

## table_bloat

**Category:** Prescriptive · **Severity:** Info / Warning · *New in v0.5*

*See also: run `dbagent analyze --explain` for detailed guidance on this rule.*

Flags scans that read dramatically more bytes than their row work warrants — a pattern consistent with dead-tuple bloat or uncleaned TOAST.

**Conditions:** node is a scan · `SharedHitBlocks + SharedReadBlocks ≥ 64` (avoid firing on tiny scans) · `bloatFactor(blocks, rows, width) ≥ 2`.

**Severity:** Info when the factor is 2–10, Warning at or above 10.

**Example finding:**

> [1] Seq Scan on rule_orders — Scan reads 4,412 blocks (34.5 MB) for 10 rows — approximately 3,613,081 bytes per row. This may indicate table bloat or dead tuples.
> Suggested: `VACUUM (ANALYZE) rule_orders;`

**What to do:** run the suggested `VACUUM (ANALYZE)` during a low-traffic window. `VACUUM FULL` is deliberately **not** suggested because it takes an exclusive lock; schedule it manually if reclaiming on-disk space is required.

