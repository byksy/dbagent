# Stats fixtures

Raw `pg_stat_statements` snapshots in JSON form. They feed
`stats.ComputeFromRows` directly, so tests that exercise aggregation,
ranking, and the recommendations engine don't need a live Postgres.

## Shape

```json
{
  "server_version": "17.2",
  "database": "mydb",
  "stats_since": "2026-04-23T08:00:00Z",
  "queries": [
    {
      "queryid": 123,
      "query": "SELECT ...",
      "calls": 42,
      "total_exec_time": 1234.5,
      "mean_exec_time": 29.4,
      "rows": 500,
      "shared_blks_hit": 800,
      "shared_blks_read": 200,
      "shared_blks_dirtied": 0,
      "shared_blks_written": 0
    },
    ...
  ]
}
```

`total_exec_time` and `mean_exec_time` are in milliseconds, matching
PostgreSQL's raw columns exactly. The per-fixture helper in
`internal/stats` loads the JSON into `pgstat.WorkloadRow` + `Meta`
and hands them to `stats.ComputeFromRows`.

## Variants

- `small_workload.json` — 10 queries, mixed read/write, healthy
  cache (used for the default golden test).
- `heavy_read_workload.json` — 10 queries, overwhelmingly reads,
  used to exercise the write-heavy negative path.
- `mixed_workload.json` — 10 queries, balanced, used to exercise
  every section of the rendered output.
