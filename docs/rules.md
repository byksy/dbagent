# Rules

`dbagent analyze` runs eight rules against every parsed plan. Each rule is either **diagnostic** (describes what's wrong) or **prescriptive** (suggests a specific fix). This page lists each rule's conditions, severity tiers, and a sample finding.

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

Rules that compute row ratios always skip `NeverExecuted` nodes, so a never-taken branch cannot inflate a rule's severity.

---

## hot_node

**Category:** Diagnostic · **Severity:** Info / Warning / Critical

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

