# Changelog

All notable changes to dbagent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.6.0] - 2026-MM-DD

### Added

- `--format markdown` for `dbagent analyze` — shareable reports with collapsible details for each finding
- Homebrew tap support: `brew install byksy/tap/dbagent`
- Precompiled binaries for Linux, macOS, and Windows (amd64 and arm64) on every release via GoReleaser
- GitHub Actions CI: automated unit tests on every push and PR, automated releases on version tags
- Issue and pull request templates

### Changed

- Revisited the pg_query_go decision with new evidence from real-world plan analysis; see `docs/decisions.md`

## [0.5.7] - 2026-04-24

### Added

- `--explain` flag for `dbagent analyze` — expanded findings with "What happened / Why it matters / What to do" guidance
- Explanation texts for all 17 rules, embedded into the binary via `go:embed`

## [0.5.6] - 2026-04-24

### Added

- `dbagent config show` — view current configuration with the password redacted
- `dbagent config path` — print the resolved config file path
- `dbagent config reset` — delete the config file (prompts for confirmation, `--force` to skip)
- `--force` flag for `dbagent init` — overwrite an existing config without prompts; non-interactive runs without `--force` now refuse to clobber existing configs

## [0.5.5] - 2026-04-24

### Added

- `dbagent stats` — workload-level analysis from `pg_stat_statements` with terminal, HTML, and JSON output
- `--order-by io` and `--order-by cache` for `dbagent top`
- `--include-system` flag on `dbagent stats` and `dbagent top` (system / maintenance queries are excluded by default)
- JSON Schema for stats output: `schemas/stats-v1.json`

## [0.5.0] - 2026-04-24

### Added

- 8 new rules: `cte_cartesian_product`, `network_overhead`, `redundant_aggregation`, `memoize_opportunity`, `unused_index_hint`, `duplicate_index`, `composite_index_extension`, `table_bloat`
- 17 rules total covering diagnostic and prescriptive analysis
- `dbagent version` reports the git-derived version through ldflags / `debug.ReadBuildInfo`
- `docs/decisions.md` — architectural decision log

## [0.4.0] - 2026-04-24

### Added

- Schema introspection via `dbagent schema` and `dbagent schema export`
- Schema-aware rules: `missing_index_on_filter`, `bitmap_and_composite`, and `row_misestimate` now use schema knowledge to filter false positives
- New rule: `fk_missing_index`
- Offline mode: `dbagent analyze --schema schema.json`

## [0.3.0] and earlier

See git history for versions prior to changelog introduction.
