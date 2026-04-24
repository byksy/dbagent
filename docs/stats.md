# `dbagent stats`

Workload-level analysis: "where is the time going across my whole database?"
Where `dbagent analyze` picks apart a single query plan, `stats`
summarises what `pg_stat_statements` has seen across every tracked
statement.

## What it does

`stats` reads `pg_stat_statements` once, aggregates the rows by total
time / call count / I/O / cache ratio, and renders a visually-polished
snapshot. It does not run `EXPLAIN`, does not write to the database,
and does not require a schema export.

Every run also emits a small set of **workload-level recommendations**
— plain-English observations distinct from plan-level findings — that
surface cross-query patterns (e.g., "one query owns 40% of total time",
"cache ratio is below 80%").

## Prerequisites

- `pg_stat_statements` enabled. Run `dbagent init --check` if unsure.
- PostgreSQL 13 or newer. PostgreSQL 14+ additionally exposes
  `pg_stat_statements_info` with the stats reset timestamp; on 13 the
  snapshot's `stats_since` stays unset and the R8 recommendation
  mentions the limitation.

## Usage

```
dbagent stats                                # default: terminal output
dbagent stats --format html > report.html    # shareable HTML
dbagent stats --format json                  # structured for CI/scripts
dbagent stats --top 20                       # more rows per section
dbagent stats --since 60                     # last 60 minutes of stats
dbagent stats --exclude '(?i)pg_catalog'     # additional user-regex skips
dbagent stats --include-system               # keep SET / SHOW / VACUUM etc.
dbagent stats --no-color                     # force plain text
```

`NO_COLOR=1` and non-TTY stdout both strip ANSI automatically.

By default, `stats` hides noise queries — pg_stat_statements probes,
`pg_catalog.` scans, transaction-control statements (BEGIN / COMMIT /
ROLLBACK / SET / SHOW), and maintenance commands (ANALYZE / VACUUM /
REINDEX / CHECKPOINT / CLUSTER). Pass `--include-system` if you
actually want to see DBA activity. The same filter applies to
`dbagent top --include-system`.

## Interpreting the output

The terminal rendering produces six sections:

1. **Database Overview** — one-line meta plus two progress bars
   (read/write split and overall cache hit). The cache bar colours
   itself green/amber/red around the 90% and 70% thresholds.
2. **Top Queries by Total Time** — by far the most useful lens.
   Each query carries a small share bar so the dominant consumer is
   obvious at a glance.
3. **Top Queries by Call Count** — "what's chatty?" — complements the
   time lens for N+1 hunting.
4. **Top Queries by I/O Reads** — orders by `shared_blks_read`, the
   metric most correlated with buffer-pool pressure.
5. **Top Queries by Low Cache Hit** — worst per-query cache ratios
   first.
6. **Recommendations** — zero or more workload-level observations
   (see below). Sorted by severity desc.

HTML and JSON expose the same data; HTML is print-friendly and
dark/light-mode aware, JSON is pinned to `schemas/stats-v1.json`.

## Recommendations reference

| ID | Severity | Fires when |
|---|---|---|
| `high_total_time_concentration` | Warning / Critical | Top query owns ≥ 30% (warn) / ≥ 50% (crit) of total DB time. |
| `write_heavy_workload` | Info | Write time > 50% of total time. |
| `low_overall_cache_hit` | Warning / Critical | Overall cache hit ratio < 90% (warn) / < 70% (crit). |
| `very_frequent_trivial_queries` | Info | A query has > 100k calls and mean time < 1ms. |
| `query_with_very_low_cache_hit` | Warning | A query has cache ratio < 50% and ≥ 1000 buffer accesses. |
| `stats_recently_reset` | Info | `pg_stat_statements_info.stats_reset` < 1h ago. |
| `no_pg_stat_statements_info` | Info | Running on PostgreSQL ≤ 13 where the info view is absent. |

Each recommendation carries a `message`, machine-readable `evidence`,
and — when useful — an `action` hint (a CLI command or operational
pointer).

## JSON schema

The JSON format is pinned to `schemas/stats-v1.json`. Output begins
with a `$schema` pointer and a `meta.schema_version` field, both of
which always match the filename. Consumers should reject documents
that don't validate against the schema file they were written for.

Stability guarantee: field names do not change within a schema
version. Breaking changes require a new file (`stats-v2.json`).

## How it relates to `dbagent analyze`

`analyze` and `stats` are complementary. When you open a new
database the natural first call is `dbagent stats`: it tells you
which queries dominate. Then you pick the hottest or most
problematic query, capture its plan with `EXPLAIN (ANALYZE,
BUFFERS, FORMAT JSON)`, and run `dbagent analyze --plan-file` to
see per-node findings and schema-aware index suggestions.

Stats never touches plans; analyze never looks at aggregated
workload metrics. Their outputs can be consumed together by the
LLM layer (Stage 9 roadmap) to narrate what's happening and what
to try next.
