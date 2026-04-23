# dbagent

*PostgreSQL query analyzer CLI — reads `pg_stat_statements`, parses `EXPLAIN` output, and suggests optimizations.*

## What it is

`dbagent` is a command-line tool for investigating PostgreSQL query performance from your terminal. In its current state it reads `pg_stat_statements` and prints the top queries by total time, mean time, or call count as a compact table. Every invocation is a single command — there is no daemon, no background agent, and nothing is ever written to your database.

The tool is being built in stages. Stage 2 adds an `analyze` command that can parse `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` output into a typed plan model — supporting live query-ids, raw SQL, and pasted/piped plan JSON. Stage 3 introduces a rule engine producing diagnostic and prescriptive findings. Later stages add schema introspection (so we don't suggest indexes that already exist), `hypopg` simulation, and optional LLM-assisted explanations.

## Requirements

- Go 1.22+ (only needed if installing from source)
- PostgreSQL 13 or newer with `pg_stat_statements` available

## Installation

**Users:**

```bash
go install github.com/byksy/dbagent/cmd/dbagent@v0.1.1
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
--order-by string        order by: total|mean|calls (default: from config)
-v, --verbose            add rows, cache hit%, and blocks-read columns
```

Example:

```bash
dbagent top --limit 10 --order-by mean --verbose
```

### `dbagent version`

Print version and runtime info:

```
dbagent v0.1.0-dev
go1.22.x linux/amd64
```

## Roadmap

1. ✓ **Stage 1 — pg_stat_statements reader (`top` command)** *(current)*
2. Stage 2 — EXPLAIN runner + typed plan model (`analyze` command, supports live queryid, raw SQL, and pasted plan JSON)
3. Stage 3 — Rule engine: first diagnostic and prescriptive findings
4. Stage 4 — Schema introspection (avoid suggesting indexes that already exist)
5. Stage 5 — Extended rule set (15-20 rules)
6. Stage 6 — Output formats (JSON, Markdown) and shareable reports
7. Stage 7 — Homebrew, precompiled binaries (GoReleaser + GitHub Actions)
8. Stage 8 — hypopg integration for index simulation
9. Stage 9 — Optional LLM layer for human-language explanations
10. Stage 10 — Multi-connection / profile support

## Contributing

Pull requests welcome. For significant changes, please open an issue first to discuss the direction. Commit messages use the imperative mood ("Add pgstat extension detection", not "Added…" or "Adds…"). Please run `make test` before pushing.

## License

MIT — see [LICENSE](LICENSE).
