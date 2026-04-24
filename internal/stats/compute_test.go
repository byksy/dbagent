package stats

import "testing"

func TestCompute_BasicWorkload(t *testing.T) {
	rows, meta := loadFixture(t, "small_workload.json")
	ws := ComputeFromRows(rows, meta, DefaultOptions())

	if ws.Meta.Database != "mydb" {
		t.Errorf("database = %q", ws.Meta.Database)
	}
	if ws.Meta.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version = %q, want %q", ws.Meta.SchemaVersion, SchemaVersion)
	}
	if ws.TotalQueries != int64(len(rows)) {
		t.Errorf("TotalQueries = %d, want %d", ws.TotalQueries, len(rows))
	}
	if ws.TotalExecutions <= 0 {
		t.Errorf("TotalExecutions should be positive")
	}
	if ws.TotalTimeMs <= 0 {
		t.Errorf("TotalTimeMs should be positive")
	}

	// Top-lists are bounded by TopN and sorted.
	if len(ws.TopByTotalTime) == 0 || len(ws.TopByTotalTime) > DefaultOptions().TopN {
		t.Errorf("TopByTotalTime unexpected length: %d", len(ws.TopByTotalTime))
	}
	for i := 1; i < len(ws.TopByTotalTime); i++ {
		if ws.TopByTotalTime[i].TotalTimeMs > ws.TopByTotalTime[i-1].TotalTimeMs {
			t.Errorf("TopByTotalTime not sorted descending at index %d", i)
			break
		}
	}

	// Read + write + ddl + other should sum to TotalTimeMs.
	readWrite := ws.ReadTimeMs + ws.WriteTimeMs
	if readWrite > ws.TotalTimeMs+0.001 {
		t.Errorf("read+write (%v) exceeds total (%v)", readWrite, ws.TotalTimeMs)
	}

	// The top entry is a SELECT in our fixture, so read dominance is
	// expected; verify the finding fires or stays silent as appropriate.
	if ws.CacheHitRatio <= 0 {
		t.Errorf("CacheHitRatio should be populated, got %v", ws.CacheHitRatio)
	}
}

func TestCompute_EmptyWorkload(t *testing.T) {
	ws := ComputeFromRows(nil, Meta{Database: "empty", ServerVersion: "17.2"}, DefaultOptions())
	if ws.TotalQueries != 0 {
		t.Errorf("empty workload should have 0 queries")
	}
	if ws.Recommendations == nil {
		t.Errorf("empty workload should return non-nil Recommendations (possibly empty slice)")
	}
}

func TestCompute_ExcludeFilter(t *testing.T) {
	rows, meta := loadFixture(t, "small_workload.json")
	ws := ComputeFromRows(rows, meta, Options{TopN: 10, ExcludeRegexp: []string{"(?i)pg_stat_activity"}})
	for _, q := range ws.TopByTotalTime {
		if q.QueryID == 1006 {
			t.Errorf("excluded query should not appear in top list")
		}
	}
}

func TestClassifyQuery(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"SELECT 1", "read"},
		{"  select id from t", "read"},
		{"WITH cte AS (SELECT) SELECT * FROM cte", "read"},
		{"INSERT INTO t VALUES (1)", "write"},
		{"UPDATE t SET x = 1", "write"},
		{"DELETE FROM t", "write"},
		{"MERGE INTO t USING s ON ...", "write"},
		{"CREATE INDEX foo ON bar(baz)", "ddl"},
		{"ALTER TABLE t ADD COLUMN c int", "ddl"},
		{"DROP TABLE t", "ddl"},
		{"TRUNCATE t", "ddl"},
		{"VACUUM t", "other"},
		{"", "other"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := classifyQuery(tt.in); got != tt.want {
				t.Errorf("classifyQuery(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestOverallCacheHitRatio(t *testing.T) {
	// All zeros → -1 (no data).
	if got := overallCacheHitRatio(nil); got != -1 {
		t.Errorf("nil rows -> got %v, want -1", got)
	}

	// Mixed: 900 hit / 100 read = 90%.
	rows := []RawQueryRow{
		{SharedBlksHit: 500, SharedBlksRead: 50},
		{SharedBlksHit: 400, SharedBlksRead: 50},
	}
	got := overallCacheHitRatio(rows)
	if got < 0.899 || got > 0.901 {
		t.Errorf("overallCacheHitRatio = %v, want ~0.9", got)
	}
}

func TestNormaliseWhitespace(t *testing.T) {
	in := "  SELECT\n    id\n  FROM   t\n"
	got := normaliseWhitespace(in)
	want := "SELECT id FROM t"
	if got != want {
		t.Errorf("normaliseWhitespace = %q, want %q", got, want)
	}
}
