package pgstat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TopOptions configures a TopQueries call.
type TopOptions struct {
	Limit   int
	OrderBy string // "total" | "mean" | "calls"
}

// QueryStat is one row from pg_stat_statements.
type QueryStat struct {
	QueryID        int64
	Query          string
	Calls          int64
	TotalExecTime  float64 // ms
	MeanExecTime   float64 // ms
	Rows           int64
	SharedBlksHit  int64
	SharedBlksRead int64
}

// allowedOrderBy maps the public OrderBy option to the ORDER BY
// expression. Must stay a whitelist — user input never reaches SQL
// verbatim. Stage 5.5 adds "io" (shared_blks_read) and "cache"
// (worst cache hit ratio first) to complement `dbagent stats`.
var allowedOrderBy = map[string]string{
	"total": "total_exec_time",
	"mean":  "mean_exec_time",
	"calls": "calls",
	"io":    "shared_blks_read",
	"cache": "CASE WHEN (shared_blks_hit + shared_blks_read) = 0 THEN 1.0 ELSE shared_blks_hit::float / (shared_blks_hit + shared_blks_read) END",
}

// orderDirection returns the trailing ASC / DESC for each allowed
// sort key. "cache" wants ASC — worst ratios come first.
var orderDirection = map[string]string{
	"total": "DESC",
	"mean":  "DESC",
	"calls": "DESC",
	"io":    "DESC",
	"cache": "ASC",
}

// topQueryTemplate is the base SQL for reading the top N queries.
// The ORDER BY expression is substituted from allowedOrderBy +
// orderDirection; the LIMIT is a parameterised placeholder.
const topQueryTemplate = `SELECT
    queryid,
    query,
    calls,
    total_exec_time,
    mean_exec_time,
    rows,
    shared_blks_hit,
    shared_blks_read
FROM pg_stat_statements
WHERE query NOT ILIKE '%%pg_stat_statements%%'
  AND query NOT ILIKE '%%pg_extension%%'
ORDER BY %s %s
LIMIT $1`

// buildTopQuery returns the SQL for TopQueries given an OrderBy option.
// An error is returned when OrderBy is not in the allowed set.
func buildTopQuery(orderBy string) (string, error) {
	col, ok := allowedOrderBy[orderBy]
	if !ok {
		return "", fmt.Errorf("pgstat: invalid order_by %q, expected one of [total, mean, calls, io, cache]", orderBy)
	}
	dir := orderDirection[orderBy]
	return fmt.Sprintf(topQueryTemplate, col, dir), nil
}

// TopQueries returns up to opts.Limit rows from pg_stat_statements,
// ordered by the option-specified column. Returns ErrExtensionMissing
// if the extension is not installed.
func TopQueries(ctx context.Context, pool *pgxpool.Pool, opts TopOptions) ([]QueryStat, error) {
	if opts.Limit < 1 {
		return nil, fmt.Errorf("pgstat: limit must be >= 1, got %d", opts.Limit)
	}
	sql, err := buildTopQuery(opts.OrderBy)
	if err != nil {
		return nil, err
	}

	rows, err := pool.Query(ctx, sql, opts.Limit)
	if err != nil {
		if isExtensionMissingErr(err) {
			return nil, fmt.Errorf("%w: %v", ErrExtensionMissing, err)
		}
		return nil, fmt.Errorf("pgstat: query top: %w", err)
	}
	defer rows.Close()

	var out []QueryStat
	for rows.Next() {
		var s QueryStat
		if err := rows.Scan(
			&s.QueryID,
			&s.Query,
			&s.Calls,
			&s.TotalExecTime,
			&s.MeanExecTime,
			&s.Rows,
			&s.SharedBlksHit,
			&s.SharedBlksRead,
		); err != nil {
			return nil, fmt.Errorf("pgstat: scan row: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		if isExtensionMissingErr(err) {
			return nil, fmt.Errorf("%w: %v", ErrExtensionMissing, err)
		}
		return nil, fmt.Errorf("pgstat: rows err: %w", err)
	}
	return out, nil
}

// isExtensionMissingErr recognises the Postgres error shapes we care
// about: the relation "pg_stat_statements" does not exist, or the view
// is not yet loaded. We pattern-match the message text since the error
// reaches us as a generic pgconn error wrapper.
func isExtensionMissingErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrExtensionMissing) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "pg_stat_statements") {
		return false
	}
	return strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "not loaded") ||
		strings.Contains(msg, "must be loaded")
}
