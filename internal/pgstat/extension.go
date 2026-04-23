// Package pgstat reads from the pg_stat_statements extension and
// reports on its installation state.
package pgstat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrExtensionMissing is returned when queries that depend on
// pg_stat_statements are attempted without the extension present.
var ErrExtensionMissing = errors.New("pg_stat_statements extension is not installed")

// ExtensionStatus reports the state of pg_stat_statements on a server.
type ExtensionStatus struct {
	ServerVersion            string
	InSharedPreloadLibraries bool
	ExtensionInstalled       bool
}

// Ready reports whether pg_stat_statements is both preloaded and
// installed as an extension in the current database.
func (s ExtensionStatus) Ready() bool {
	return s.InSharedPreloadLibraries && s.ExtensionInstalled
}

// CheckExtension runs three queries to determine the state of
// pg_stat_statements on the current server and database.
func CheckExtension(ctx context.Context, pool *pgxpool.Pool) (ExtensionStatus, error) {
	var status ExtensionStatus

	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version')`).Scan(&status.ServerVersion); err != nil {
		return status, fmt.Errorf("pgstat: read server_version: %w", err)
	}

	var preload string
	if err := pool.QueryRow(ctx, `SELECT current_setting('shared_preload_libraries')`).Scan(&preload); err != nil {
		return status, fmt.Errorf("pgstat: read shared_preload_libraries: %w", err)
	}
	status.InSharedPreloadLibraries = isInPreload(preload)

	var present int
	err := pool.QueryRow(ctx,
		`SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements'`,
	).Scan(&present)
	switch {
	case err == nil:
		status.ExtensionInstalled = true
	case errors.Is(err, pgx.ErrNoRows):
		status.ExtensionInstalled = false
	default:
		return status, fmt.Errorf("pgstat: query pg_extension: %w", err)
	}

	return status, nil
}

// isInPreload reports whether pg_stat_statements appears in a raw
// shared_preload_libraries setting. The setting is a CSV with
// arbitrary surrounding whitespace per entry; comparison is
// case-insensitive.
func isInPreload(setting string) bool {
	if setting == "" {
		return false
	}
	for _, entry := range strings.Split(setting, ",") {
		if strings.EqualFold(strings.TrimSpace(entry), "pg_stat_statements") {
			return true
		}
	}
	return false
}

