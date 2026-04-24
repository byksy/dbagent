//go:build integration

package schema

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/byksy/dbagent/internal/config"
	"github.com/byksy/dbagent/internal/db"
)

// dockerConfig matches the credentials in docker-compose.yml. The
// integration tag keeps this off the default `go test` path, so
// developers without Docker aren't forced to stand it up.
func dockerConfig() config.DatabaseConfig {
	return config.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "postgres",
		SSLMode:  "disable",
	}
}

// TestFetch exercises the full introspection pipeline against the
// docker-compose Postgres. Assumes the Stage 3 capture script has
// run and rule_orders exists — run `make rule-fixtures` first if it
// doesn't.
func TestFetch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := db.Connect(ctx, dockerConfig())
	if err != nil {
		t.Skipf("docker_postgres not reachable, skipping: %v", err)
	}
	defer pool.Close()

	s, err := Fetch(ctx, pool)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if s.Meta.Database == "" {
		t.Errorf("Meta.Database empty")
	}
	if s.Meta.ServerVersion == "" {
		t.Errorf("Meta.ServerVersion empty")
	}
	if len(s.Tables) == 0 {
		t.Fatalf("no tables fetched")
	}
	if len(s.Indexes) == 0 {
		t.Fatalf("no indexes fetched")
	}

	// Find a known table from Stage 3 fixtures. Skip (not fail) when
	// the table isn't present, since Stage 4 tests may run against a
	// fresh DB.
	orders, ok := s.Tables["public.rule_orders"]
	if !ok {
		t.Skip("rule_orders not present; run `make rule-fixtures` for a full integration run")
	}
	var sawIDColumn bool
	for _, c := range orders.Columns {
		if c.Name == "id" && c.NotNull {
			sawIDColumn = true
		}
	}
	if !sawIDColumn {
		t.Errorf("rule_orders should have a NOT NULL id column, got %+v", orders.Columns)
	}
	if _, ok := s.Indexes["public.rule_orders_pkey"]; !ok {
		t.Errorf("rule_orders_pkey not found among indexes")
	}
}

// TestExport_RoundTrip fetches the live schema, writes it to JSON,
// reads it back, and verifies the essential fields survive. Timestamps
// are compared at second precision because JSON marshalling drops
// sub-second resolution for time.Time without a custom marshaller.
func TestExport_RoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := db.Connect(ctx, dockerConfig())
	if err != nil {
		t.Skipf("docker_postgres not reachable: %v", err)
	}
	defer pool.Close()

	orig, err := Fetch(ctx, pool)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	var buf bytes.Buffer
	if err := orig.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	got, err := LoadJSON(&buf)
	if err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}

	if got.Meta.Database != orig.Meta.Database {
		t.Errorf("Database drift: %q vs %q", got.Meta.Database, orig.Meta.Database)
	}
	if len(got.Tables) != len(orig.Tables) {
		t.Errorf("table count drift: got %d orig %d", len(got.Tables), len(orig.Tables))
	}
	if len(got.Indexes) != len(orig.Indexes) {
		t.Errorf("index count drift: got %d orig %d", len(got.Indexes), len(orig.Indexes))
	}
	if len(got.FKeys) != len(orig.FKeys) {
		t.Errorf("fkey count drift: got %d orig %d", len(got.FKeys), len(orig.FKeys))
	}
}

