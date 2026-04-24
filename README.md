# dbagent

*PostgreSQL query analyzer CLI — reads `pg_stat_statements`, parses `EXPLAIN` output, and suggests optimizations.*

## What it is

`dbagent` is a command-line tool for investigating PostgreSQL query performance from your terminal. It reads `pg_stat_statements` to show the top queries on a live server (`dbagent top`), produces a colored workload-level snapshot with progress bars and recommendations (`dbagent stats`), parses `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` output into a typed plan tree, and runs **seventeen** schema-aware diagnostic and prescriptive rules (see [`docs/rules.md`](docs/rules.md)) to point out hot nodes, misestimates, missing indexes, CTE-rescan traps, bloat signals, and more (`dbagent analyze`). It can also export a full schema snapshot (`dbagent schema export`) for offline analysis away from the live database. Every invocation is a single command — there is no daemon, no background agent, and nothing is ever written to your database.

The tool is being built in stages. Upcoming work includes an expanded rule set, `hypopg` simulation, and optional LLM-assisted explanations.

## Requirements

- Go 1.22+ (only needed if installing from source)
- PostgreSQL 13 or newer with `pg_stat_statements` available

## Installation

**Users:**

```bash
go install github.com/byksy/dbagent/cmd/dbagent@v0.5.5
```

(Until the module is mature, pin to a specific tag. `@latest` will resolve
to the newest tagged release.)

Homebrew, precompiled binaries, and a Docker image are *coming in v0.3 — see the roadmap*.

**From source (contributors):**

```bash
git clone https://github.com/byksy/dbagent.git
cd dbagent
make docker-up          # starts Postgres 17 with pg_stat_statements preloaded
make build              # produces ./bin/dbagent
./bin/dbagent init
```

## Quick start

```bash
dbagent init            # collect DB connection details, save config
# follow the prompts
dbagent top             # print top queries from pg_stat_statements
```

## Enabling pg_stat_statements
<a id="enabling-pg_stat_statements"></a>

`pg_stat_statements` ships with PostgreSQL but must be explicitly enabled. The steps differ between self-managed and managed Postgres.

**Self-managed (your own `postgresql.conf`):**

1. Add (or edit) the `shared_preload_libraries` line:

    ```
    shared_preload_libraries = 'pg_stat_statements'
    ```

2. Restart PostgreSQL (e.g., `sudo systemctl restart postgresql`).
3. Connect to the target database as a superuser and run:

    ```sql
    CREATE EXTENSION pg_stat_statements;
    ```

**Managed databases** — enable the extension from your provider's dashboard:

- **Amazon RDS / Aurora** — modify the parameter group to include `pg_stat_statements` in `shared_preload_libraries`, then `CREATE EXTENSION`.
- **Google Cloud SQL** — set the `cloudsql.enable_pg_stat_statements` flag, then `CREATE EXTENSION`.
- **Neon** — enabled by default on most plans; just `CREATE EXTENSION`.
- **Supabase** — available under *Database → Extensions*.

Consult your provider's docs for the exact steps.

## Configuration

By default, `dbagent` reads its config from:

- **Linux / BSD:** `$XDG_CONFIG_HOME/dbagent/config.yaml`, falling back to `~/.config/dbagent/config.yaml`
- **macOS:** `~/.config/dbagent/config.yaml` (XDG convention)
- **Windows:** the OS-native application-config directory (e.g., `%AppData%\dbagent\config.yaml`)

Override with `--config /path/to/config.yaml`.

**Schema:**

```yaml
database:
  host: localhost
  port: 5432
  user: postgres
  password: postgres
  database: postgres
  sslmode: disable          # one of: disable, require, verify-ca, verify-full

output:
  limit: 20                 # 1-500
  order_by: total           # one of: total, mean, calls

log:
  level: info               # one of: debug, info, warn, error
```

**Environment overrides** use the `DBAGENT_` prefix and underscore-separated keys, e.g.:

```bash
export DBAGENT_DATABASE_PASSWORD="hunter2"
export DBAGENT_DATABASE_HOST=db.internal
```

## Usage

### `dbagent init`

Create or verify the database configuration.

```
--host string            database host
--port int               database port
--database string        database name
--user string            database user
--password string        database password (prefer --password-env)
--password-env string    environment variable holding the database password
--sslmode string         sslmode: disable|require|verify-ca|verify-full
--no-prompt              fail instead of prompting for missing values
--check                  check existing config, do not modify
--force                  overwrite an existing config without prompting
```

Example — non-interactive init reading the password from the environment:

```bash
DBAGENT_PW="s3cret" dbagent init \
  --host db.internal --port 5432 --user app --database analytics \
  --sslmode require --password-env DBAGENT_PW --no-prompt
```

### `dbagent top`

Print the top queries from `pg_stat_statements`.

```
--limit int              number of queries to show (default: from config)
--order-by string        order by: total|mean|calls|io|cache (default: from config)
--include-system         include pg_catalog / SET / SHOW / VACUUM / ANALYZE queries (excluded by default)
-v, --verbose            add rows, cache hit%, and blocks-read columns
```

Example:

```bash
dbagent top --limit 10 --order-by mean --verbose
```

### `dbagent analyze`

Parse and render an `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` plan from a file or stdin. Offline — no DB connection needed. Runs eight built-in rules that emit diagnostic and prescriptive findings (see [`docs/rules.md`](docs/rules.md) for the full list).

```
--plan-file string       path to EXPLAIN JSON file; empty means read from stdin
--format string          output format: tree (default), table, json
--schema string          schema.json for offline analysis; --schema= opts out of live fetch
--fail-on string         exit code 7 if any finding reaches this severity: info|warning|critical
```

Example output:

```
Plan (total: 125.3ms, planning: 0.3ms, execution: 125.0ms)

[1] Seq Scan on rule_orders  rows=5,000  time=120.0ms  loops=1  ✗
    filter=(status = 'shipped'::text)  filter_removed=495,000
    buffers: shared hit=8,000 read=0

Summary
  Slowest node (exclusive):  [1] Seq Scan on rule_orders — 120.0ms (96% of total)

Findings
  CRITICAL  [1]
            └─ missing_index_on_filter
               Filter removes 99% of rows scanned (495000 rows). Consider an index to push the predicate down.
               Suggested: CREATE INDEX ON rule_orders (status);

1 findings (1 critical)
```

Examples:

```bash
# From a file
dbagent analyze --plan-file plan.json

# From stdin
psql -U me -d mydb -c "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) SELECT ..." \
    | dbagent analyze

# Alternative formats
dbagent analyze --plan-file plan.json --format table
dbagent analyze --plan-file plan.json --format json | jq '.summary.findings'

# CI gate: fail the build on any critical finding
dbagent analyze --plan-file plan.json --fail-on critical
```

Capture plans from PostgreSQL using:

```sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) <your query>
```

Only JSON format is supported in this version. TEXT/YAML/XML will produce a clear error.

### `dbagent schema`

Print a human-readable schema overview — tables, indexes, and foreign keys — using the live database connection.

```bash
dbagent schema
```

Use this to verify that `dbagent` sees the schema you expect before running `analyze`.

### `dbagent schema export`

Write the full schema as JSON to stdout. Useful when you want to run `analyze` against plans captured from a database you can't (or shouldn't) connect to directly:

```bash
# On a host with DB access
dbagent schema export > schema.json

# Elsewhere / later
dbagent analyze --plan-file plan.json --schema schema.json
```

The export carries a timestamp. `analyze --schema` prints a warning when the export is older than 24 hours, but still proceeds.

### `dbagent stats` — workload analysis

Reads `pg_stat_statements` and renders a colored, polished snapshot
of where time is going across the whole database. Complements
`analyze`: use `stats` to pick the hottest query, then `analyze` to
dissect its plan.

```bash
dbagent stats                                # default: terminal
dbagent stats --format html > report.html    # shareable report
dbagent stats --format json | jq '.recommendations'
dbagent stats --top 20                       # rows per section
```

Flags:

```
--format string           terminal (default) | html | json
--top int                 rows per section (default 10, max 50)
--since int               filter: stats from last N minutes (0 = all)
--exclude strings         regex patterns to skip
--include-system          include pg_catalog / SET / SHOW / VACUUM / ANALYZE queries (excluded by default)
--no-color                force plain text even on a TTY
```

The JSON form is pinned to [`schemas/stats-v1.json`](schemas/stats-v1.json)
so it's safe to consume from CI pipelines and future dashboards. See
[`docs/stats.md`](docs/stats.md) for the recommendation catalog and
interpretation guide.

### `dbagent config`

Manage your dbagent configuration without hand-editing the YAML file.

```bash
# Show current config (password redacted)
dbagent config show

# Print the config file path (one line to stdout, pipeable)
dbagent config path

# Delete the config file (prompts for confirmation)
dbagent config reset

# Delete without prompting — required in CI / non-TTY contexts
dbagent config reset --force
```

To create a fresh config interactively, run `dbagent init`. To
overwrite an existing config non-interactively, combine `init` with
`--force`:

```bash
dbagent init --force --no-prompt \
  --host db.prod --port 5432 --user ro \
  --database app --password-env DB_PASS --sslmode require
```

Without `--force`, `init` refuses to clobber an existing config
whenever it is running non-interactively — stdin is not a terminal
or `--no-prompt` is set — so automation can't silently replace a
good config. Interactive runs prompt before overwriting.

### `dbagent version`

Print version and runtime info:

```
dbagent v0.5.5
go1.22.x linux/amd64
```

## Roadmap

1. ✓ Stage 1 — pg_stat_statements reader (`top` command)
2. ✓ Stage 2 — EXPLAIN plan parser, `analyze` command (offline tree/table/JSON rendering + summary)
3. ✓ Stage 3 — Rule engine: first eight diagnostic and prescriptive findings
4. ✓ Stage 4 — Schema introspection, `schema` + `schema export` commands, schema-aware rules, `fk_missing_index` finding
5. ✓ Stage 5 — Expanded rule catalog to 17 rules + `docs/decisions.md`
5.5. ✓ **Stage 5.5 — `dbagent stats` workload analysis with colored terminal / HTML / JSON output** *(current — see [`docs/stats.md`](docs/stats.md))*
6. Stage 6 — Output formats (JSON, Markdown) and shareable reports
7. Stage 7 — Homebrew, precompiled binaries (GoReleaser + GitHub Actions)
8. Stage 8 — hypopg integration for index simulation
9. Stage 9 — Optional LLM layer for human-language explanations
10. Stage 10 — Multi-connection / profile support

## Contributing

Pull requests welcome. For significant changes, please open an issue first to discuss the direction. Commit messages use the imperative mood ("Add pgstat extension detection", not "Added…" or "Adds…"). Please run `make test` before pushing.

## License

MIT — see [LICENSE](LICENSE).
