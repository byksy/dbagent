package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/byksy/dbagent/internal/pgstat"
	"github.com/byksy/dbagent/internal/stats"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain forces lipgloss into ASCII mode for this package so
// golden rendering tests don't swing on the runner's terminal
// detection. The style package has its own TestMain but each Go
// test binary gets its own init path; repeating here is cheap and
// keeps the tests robust.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}

var updateStats = flag.Bool("update-stats", false, "update stats golden files")

const (
	statsFixtureDir = "../../testdata/stats"
	statsGoldenDir  = "../../testdata/golden/stats"
)

// statsFixtureShape mirrors the on-disk JSON in testdata/stats/.
// Duplicated from the stats package's private loader so this test
// package doesn't need to import unexported helpers.
type statsFixtureShape struct {
	ServerVersion string    `json:"server_version"`
	Database      string    `json:"database"`
	StatsSince    time.Time `json:"stats_since"`
	Queries       []struct {
		QueryID           int64   `json:"queryid"`
		Query             string  `json:"query"`
		Calls             int64   `json:"calls"`
		TotalExecTime     float64 `json:"total_exec_time"`
		MeanExecTime      float64 `json:"mean_exec_time"`
		Rows              int64   `json:"rows"`
		SharedBlksHit     int64   `json:"shared_blks_hit"`
		SharedBlksRead    int64   `json:"shared_blks_read"`
		SharedBlksDirtied int64   `json:"shared_blks_dirtied"`
		SharedBlksWritten int64   `json:"shared_blks_written"`
	} `json:"queries"`
}

// loadStatsFixture reads a fixture and returns a WorkloadStats. The
// snapshot_at timestamp is pinned so the terminal/html/json goldens
// don't shift between runs.
func loadStatsFixture(t *testing.T, name string) *stats.WorkloadStats {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(statsFixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f statsFixtureShape
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	rows := make([]stats.RawQueryRow, 0, len(f.Queries))
	for _, q := range f.Queries {
		rows = append(rows, pgstat.WorkloadRow{
			QueryID:           q.QueryID,
			Query:             q.Query,
			Calls:             q.Calls,
			TotalExecTimeMs:   q.TotalExecTime,
			MeanExecTimeMs:    q.MeanExecTime,
			Rows:              q.Rows,
			SharedBlksHit:     q.SharedBlksHit,
			SharedBlksRead:    q.SharedBlksRead,
			SharedBlksDirtied: q.SharedBlksDirtied,
			SharedBlksWritten: q.SharedBlksWritten,
		})
	}
	meta := stats.Meta{
		Database:       f.Database,
		ServerVersion:  f.ServerVersion,
		SnapshotAt:     time.Date(2026, 4, 24, 13, 52, 15, 0, time.UTC),
		StatsSince:     f.StatsSince,
		DBAgentVersion: "test",
	}
	ws := stats.ComputeFromRows(rows, meta, stats.DefaultOptions())
	return ws
}

// writeGolden handles the update vs. compare dance the Stage 2+3
// tests pioneered, extended with a stats-specific --update-stats
// flag so running the full rule test suite with -update doesn't
// accidentally regenerate visual goldens.
func writeGolden(t *testing.T, path, got string) {
	t.Helper()
	got = strings.ReplaceAll(got, "\r\n", "\n")
	if *updateStats {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update-stats to create)", path, err)
	}
	if string(want) != got {
		t.Errorf("golden mismatch at %s\n---want---\n%s\n---got---\n%s", path, want, got)
	}
}

func TestRenderTerminal_GoldenSmall(t *testing.T) {
	ws := loadStatsFixture(t, "small_workload.json")
	var buf bytes.Buffer
	if err := renderStatsTerminal(&buf, ws, 80); err != nil {
		t.Fatalf("render: %v", err)
	}
	writeGolden(t, filepath.Join(statsGoldenDir, "terminal_small.txt"), buf.String())
}

func TestRenderTerminal_WideLayout(t *testing.T) {
	ws := loadStatsFixture(t, "mixed_workload.json")
	var buf bytes.Buffer
	if err := renderStatsTerminal(&buf, ws, 120); err != nil {
		t.Fatalf("render: %v", err)
	}
	// Sanity: wide layout emits the expected section titles and at
	// least one data row. Full output pinned via the small-layout
	// golden.
	s := buf.String()
	for _, want := range []string{"Database Overview", "Top Queries by Total Time", "Recommendations"} {
		if !strings.Contains(s, want) {
			t.Errorf("wide render missing %q:\n%s", want, s)
		}
	}
}

func TestRenderTerminal_NarrowFallback(t *testing.T) {
	ws := loadStatsFixture(t, "small_workload.json")
	var buf bytes.Buffer
	if err := renderStatsTerminal(&buf, ws, 50); err != nil {
		t.Fatalf("render: %v", err)
	}
	// Compact fallback has no rounded-border glyphs but still shows
	// section titles.
	out := buf.String()
	if strings.ContainsAny(out, "╭╰╮╯") {
		t.Errorf("narrow render should skip borders, got:\n%s", out)
	}
	if !strings.Contains(out, "Database Overview") {
		t.Errorf("narrow render missing section title:\n%s", out)
	}
}

func TestRenderJSON_StructuralValidity(t *testing.T) {
	ws := loadStatsFixture(t, "small_workload.json")
	var buf bytes.Buffer
	if err := renderStatsJSON(&buf, ws); err != nil {
		t.Fatalf("render: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	// Required top-level keys per schemas/stats-v1.json.
	required := []string{
		"$schema", "meta",
		"total_queries", "total_executions", "total_time_ms",
		"read_time_ms", "write_time_ms", "cache_hit_ratio",
		"top_by_total_time", "top_by_call_count", "top_by_mean_time",
		"top_by_io", "top_by_low_cache", "recommendations",
	}
	for _, k := range required {
		if _, ok := out[k]; !ok {
			t.Errorf("JSON missing required key %q", k)
		}
	}
	// Schema version pinned.
	meta := out["meta"].(map[string]any)
	if meta["schema_version"] != stats.SchemaVersion {
		t.Errorf("schema_version = %v, want %v", meta["schema_version"], stats.SchemaVersion)
	}
	// Query kind values must be in the enum.
	for _, g := range out["top_by_total_time"].([]any) {
		qg := g.(map[string]any)
		kind := qg["query_kind"].(string)
		switch kind {
		case "read", "write", "ddl", "other":
		default:
			t.Errorf("unexpected query_kind %q", kind)
		}
	}
	// Recommendation severities stay in the enum.
	for _, r := range out["recommendations"].([]any) {
		rec := r.(map[string]any)
		sev := rec["severity"].(string)
		switch sev {
		case "info", "warning", "critical":
		default:
			t.Errorf("unexpected recommendation severity %q", sev)
		}
	}
}

func TestRenderJSON_IsStableAcrossRuns(t *testing.T) {
	ws := loadStatsFixture(t, "small_workload.json")
	var a, b bytes.Buffer
	if err := renderStatsJSON(&a, ws); err != nil {
		t.Fatal(err)
	}
	if err := renderStatsJSON(&b, ws); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Errorf("JSON rendering should be deterministic across calls")
	}
}

func TestRenderHTML_ContainsTOCAndSections(t *testing.T) {
	ws := loadStatsFixture(t, "small_workload.json")
	var buf bytes.Buffer
	if err := renderStatsHTML(&buf, ws); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"<!doctype html>",
		"<title>dbagent stats",
		`href="#overview"`,
		`href="#top-total-time"`,
		`href="#recommendations"`,
		"prefers-color-scheme: dark",
		"@media print",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}
