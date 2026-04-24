package pgstat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WorkloadRow is the flat pg_stat_statements shape consumed by
// internal/stats's aggregation code. Stage 1's top.go fetches the
// same catalog with a different column set and top-N limit; keeping
// the workload fetch separate avoids entangling two callers with
// different filter / ordering needs.
type WorkloadRow struct {
	QueryID           int64
	Query             string
	Calls             int64
	TotalExecTimeMs   float64
	MeanExecTimeMs    float64
	Rows              int64
	SharedBlksHit     int64
	SharedBlksRead    int64
	SharedBlksDirtied int64
	SharedBlksWritten int64
}

// WorkloadMeta is the provenance block returned alongside the rows.
// ServerVersion is the raw pg_config value, StatsSince is the
// timestamp from pg_stat_statements_info (zero value if unavailable
// on older PostgreSQL — stats code surfaces this via recommendation
// R8 rather than as an error).
type WorkloadMeta struct {
	Database      string
	ServerVersion string
	SnapshotAt    time.Time
	StatsSince    time.Time
}

// WorkloadOptions tunes FetchWorkload's SQL filters. Empty values
// mean no filter.
type WorkloadOptions struct {
	SinceMinutes int
	// IncludeSystem disables the default noise filter (pg_catalog
	// scans, transaction-control, maintenance commands, dbagent's
	// own probes). Off by default so `dbagent stats` shows user
	// workload.
	IncludeSystem bool
}

// FetchWorkload pulls the full pg_stat_statements snapshot plus the
// provenance metadata stats.Compute needs. Returns ErrExtensionMissing
// if pg_stat_statements is not available. Missing
// pg_stat_statements_info (PostgreSQL 13 and older) is NOT an error
// — StatsSince stays zero and the caller surfaces R8.
func FetchWorkload(ctx context.Context, pool *pgxpool.Pool, opts WorkloadOptions) ([]WorkloadRow, WorkloadMeta, error) {
	var meta WorkloadMeta
	meta.SnapshotAt = time.Now().UTC()

	if err := pool.QueryRow(ctx,
		`SELECT current_database(), current_setting('server_version')`,
	).Scan(&meta.Database, &meta.ServerVersion); err != nil {
		return nil, meta, fmt.Errorf("pgstat: read server meta: %w", err)
	}

	// pg_stat_statements_info only exists in PostgreSQL 14+. A query
	// against it on older servers errors with "relation does not
	// exist". We swallow that specific shape rather than letting it
	// bubble up so stats still runs on PG13.
	var since time.Time
	err := pool.QueryRow(ctx, `SELECT stats_reset FROM pg_stat_statements_info`).Scan(&since)
	switch {
	case err == nil:
		meta.StatsSince = since.UTC()
	case errors.Is(err, pgx.ErrNoRows):
		// view exists but empty: leave zero
	default:
		// missing view on PG13 surfaces here; keep StatsSince zero.
		if !isMissingRelationErr(err) {
			return nil, meta, fmt.Errorf("pgstat: read stats_reset: %w", err)
		}
	}

	query := workloadSQL(opts)
	rows, err := pool.Query(ctx, query)
	if err != nil {
		if isExtensionMissingErr(err) {
			return nil, meta, fmt.Errorf("%w: %v", ErrExtensionMissing, err)
		}
		return nil, meta, fmt.Errorf("pgstat: query workload: %w", err)
	}
	defer rows.Close()

	var out []WorkloadRow
	for rows.Next() {
		var r WorkloadRow
		if err := rows.Scan(
			&r.QueryID, &r.Query,
			&r.Calls, &r.TotalExecTimeMs, &r.MeanExecTimeMs, &r.Rows,
			&r.SharedBlksHit, &r.SharedBlksRead,
			&r.SharedBlksDirtied, &r.SharedBlksWritten,
		); err != nil {
			return nil, meta, fmt.Errorf("pgstat: scan workload row: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		if isExtensionMissingErr(err) {
			return nil, meta, fmt.Errorf("%w: %v", ErrExtensionMissing, err)
		}
		return nil, meta, fmt.Errorf("pgstat: rows: %w", err)
	}
	return out, meta, nil
}

// workloadSQL builds the pg_stat_statements select statement.
// SinceMinutes is intentionally not translated into a WHERE clause:
// pg_stat_statements has no per-statement "last_seen" column, so a
// real rolling filter isn't possible at the SQL level. The option
// is accepted for forward compatibility so callers can grow the
// filter once PostgreSQL exposes a suitable timestamp.
func workloadSQL(opts WorkloadOptions) string {
	return `
SELECT
    queryid,
    query,
    calls,
    total_exec_time,
    mean_exec_time,
    rows,
    shared_blks_hit,
    shared_blks_read,
    shared_blks_dirtied,
    shared_blks_written
FROM pg_stat_statements
WHERE TRUE` + systemQueryFilterSQL(opts.IncludeSystem) + `
ORDER BY total_exec_time DESC`
}

// isMissingRelationErr detects the "relation does not exist" error
// shape that older PG versions produce when we probe a view that
// was added later. Keeps FetchWorkload working across 13+.
func isMissingRelationErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not exist")
}
