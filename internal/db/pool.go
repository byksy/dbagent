// Package db owns the pgx connection pool lifecycle.
package db

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/byksy/dbagent/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pgx connection pool against the configured database
// and pings it. Returns a pool ready for use; the caller must call
// pool.Close() when done.
func Connect(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	connString := buildConnString(cfg)

	pgxCfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("connect to %s@%s:%d/%s: parse config: %w",
			cfg.User, cfg.Host, cfg.Port, cfg.Database, err)
	}

	pgxCfg.MaxConns = 10
	pgxCfg.MinConns = 1
	pgxCfg.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("connect to %s@%s:%d/%s: %w",
			cfg.User, cfg.Host, cfg.Port, cfg.Database, err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to %s@%s:%d/%s: ping: %w",
			cfg.User, cfg.Host, cfg.Port, cfg.Database, err)
	}
	return pool, nil
}

// buildConnString produces a libpq-style URL. Password is URL-escaped so
// that special characters do not break the URI. Never log this string
// directly without redaction.
func buildConnString(cfg config.DatabaseConfig) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Path:   "/" + cfg.Database,
	}
	q := url.Values{}
	if cfg.SSLMode != "" {
		q.Set("sslmode", cfg.SSLMode)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
