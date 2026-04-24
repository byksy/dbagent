package stats

import (
	"testing"
	"time"

	"github.com/byksy/dbagent/internal/rules"
)

// findRec returns the first recommendation with the given ID, or nil.
func findRec(recs []Recommendation, id string) *Recommendation {
	for i := range recs {
		if recs[i].ID == id {
			return &recs[i]
		}
	}
	return nil
}

func TestR1_HighTotalTimeConcentration(t *testing.T) {
	// Top query owns 60% of total time → Critical.
	rows := []RawQueryRow{
		{QueryID: 1, Query: "SELECT 1", Calls: 10, TotalExecTimeMs: 600, MeanExecTimeMs: 60},
		{QueryID: 2, Query: "SELECT 2", Calls: 10, TotalExecTimeMs: 250, MeanExecTimeMs: 25},
		{QueryID: 3, Query: "SELECT 3", Calls: 10, TotalExecTimeMs: 150, MeanExecTimeMs: 15},
	}
	ws := ComputeFromRows(rows, Meta{ServerVersion: "17.2"}, DefaultOptions())
	rec := findRec(ws.Recommendations, "high_total_time_concentration")
	if rec == nil {
		t.Fatalf("expected high_total_time_concentration, got %+v", ws.Recommendations)
	}
	if rec.Severity != rules.SeverityCritical {
		t.Errorf("severity = %v, want Critical", rec.Severity)
	}
}

func TestR1_BelowThreshold(t *testing.T) {
	// Top query at 20% → no finding.
	rows := []RawQueryRow{
		{QueryID: 1, Query: "SELECT", Calls: 1, TotalExecTimeMs: 200, MeanExecTimeMs: 200},
		{QueryID: 2, Query: "SELECT", Calls: 1, TotalExecTimeMs: 200, MeanExecTimeMs: 200},
		{QueryID: 3, Query: "SELECT", Calls: 1, TotalExecTimeMs: 200, MeanExecTimeMs: 200},
		{QueryID: 4, Query: "SELECT", Calls: 1, TotalExecTimeMs: 200, MeanExecTimeMs: 200},
		{QueryID: 5, Query: "SELECT", Calls: 1, TotalExecTimeMs: 200, MeanExecTimeMs: 200},
	}
	ws := ComputeFromRows(rows, Meta{ServerVersion: "17.2"}, DefaultOptions())
	if rec := findRec(ws.Recommendations, "high_total_time_concentration"); rec != nil {
		t.Errorf("should not fire at 20%% share, got %+v", rec)
	}
}

func TestR2_WriteHeavy(t *testing.T) {
	rows := []RawQueryRow{
		{QueryID: 1, Query: "INSERT INTO t VALUES (1)", Calls: 1, TotalExecTimeMs: 800, MeanExecTimeMs: 800},
		{QueryID: 2, Query: "SELECT 1", Calls: 1, TotalExecTimeMs: 100, MeanExecTimeMs: 100},
	}
	ws := ComputeFromRows(rows, Meta{ServerVersion: "17.2"}, DefaultOptions())
	if rec := findRec(ws.Recommendations, "write_heavy_workload"); rec == nil {
		t.Errorf("expected write_heavy_workload on %+v", ws.Recommendations)
	}
}

func TestR2_ReadHeavyIsSilent(t *testing.T) {
	rows, meta := loadFixture(t, "heavy_read_workload.json")
	ws := ComputeFromRows(rows, meta, DefaultOptions())
	if rec := findRec(ws.Recommendations, "write_heavy_workload"); rec != nil {
		t.Errorf("read-heavy workload should not fire write_heavy_workload, got %+v", rec)
	}
}

func TestR3_LowOverallCache(t *testing.T) {
	// Forces a 60% ratio (< 70% → Critical).
	rows := []RawQueryRow{
		{QueryID: 1, Query: "SELECT", Calls: 1, TotalExecTimeMs: 100, SharedBlksHit: 600, SharedBlksRead: 400},
	}
	ws := ComputeFromRows(rows, Meta{ServerVersion: "17.2"}, DefaultOptions())
	rec := findRec(ws.Recommendations, "low_overall_cache_hit")
	if rec == nil {
		t.Fatalf("expected low_overall_cache_hit")
	}
	if rec.Severity != rules.SeverityCritical {
		t.Errorf("severity = %v, want Critical", rec.Severity)
	}
}

func TestR3_HighCacheIsSilent(t *testing.T) {
	rows := []RawQueryRow{
		{QueryID: 1, Query: "SELECT", Calls: 1, TotalExecTimeMs: 100, SharedBlksHit: 9800, SharedBlksRead: 200},
	}
	ws := ComputeFromRows(rows, Meta{ServerVersion: "17.2"}, DefaultOptions())
	if rec := findRec(ws.Recommendations, "low_overall_cache_hit"); rec != nil {
		t.Errorf("98%% cache ratio should not fire low_overall_cache_hit")
	}
}

func TestR5_VeryFrequentTrivialQueries(t *testing.T) {
	rows := []RawQueryRow{
		{QueryID: 42, Query: "SELECT 1", Calls: 500_000, TotalExecTimeMs: 100, MeanExecTimeMs: 0.2},
		{QueryID: 43, Query: "SELECT 2", Calls: 10, TotalExecTimeMs: 50, MeanExecTimeMs: 5},
	}
	ws := ComputeFromRows(rows, Meta{ServerVersion: "17.2"}, DefaultOptions())
	rec := findRec(ws.Recommendations, "very_frequent_trivial_queries")
	if rec == nil {
		t.Fatalf("expected very_frequent_trivial_queries")
	}
	if rec.Evidence["query_id"].(int64) != 42 {
		t.Errorf("pointed at wrong query: %+v", rec.Evidence)
	}
}

func TestR5_SlowerQueryIsSilent(t *testing.T) {
	rows := []RawQueryRow{
		{QueryID: 42, Query: "SELECT 1", Calls: 500_000, TotalExecTimeMs: 100, MeanExecTimeMs: 5.0},
	}
	ws := ComputeFromRows(rows, Meta{ServerVersion: "17.2"}, DefaultOptions())
	if rec := findRec(ws.Recommendations, "very_frequent_trivial_queries"); rec != nil {
		t.Errorf("5ms mean should not trigger trivial-frequent: %+v", rec)
	}
}

func TestR6_LowPerQueryCache(t *testing.T) {
	rows := []RawQueryRow{
		{QueryID: 1, Query: "SELECT", Calls: 1, TotalExecTimeMs: 100, SharedBlksHit: 100, SharedBlksRead: 9900},
		{QueryID: 2, Query: "SELECT", Calls: 1, TotalExecTimeMs: 100, SharedBlksHit: 9900, SharedBlksRead: 100},
	}
	ws := ComputeFromRows(rows, Meta{ServerVersion: "17.2"}, DefaultOptions())
	rec := findRec(ws.Recommendations, "query_with_very_low_cache_hit")
	if rec == nil {
		t.Fatalf("expected query_with_very_low_cache_hit")
	}
	if rec.Evidence["query_id"].(int64) != 1 {
		t.Errorf("should point at the low-cache query, got %+v", rec.Evidence)
	}
}

func TestR7_StatsRecentlyReset(t *testing.T) {
	meta := Meta{ServerVersion: "17.2", StatsSince: time.Now().Add(-10 * time.Minute)}
	ws := ComputeFromRows(nil, meta, DefaultOptions())
	if rec := findRec(ws.Recommendations, "stats_recently_reset"); rec == nil {
		t.Errorf("10-minute-old reset should fire R7")
	}
}

func TestR7_OldStatsNoFire(t *testing.T) {
	meta := Meta{ServerVersion: "17.2", StatsSince: time.Now().Add(-5 * time.Hour)}
	ws := ComputeFromRows(nil, meta, DefaultOptions())
	if rec := findRec(ws.Recommendations, "stats_recently_reset"); rec != nil {
		t.Errorf("5-hour-old reset should NOT fire R7")
	}
}

func TestR8_NoPgStatInfoOnOldServer(t *testing.T) {
	meta := Meta{ServerVersion: "13.4", StatsSince: time.Time{}}
	ws := ComputeFromRows(nil, meta, DefaultOptions())
	if rec := findRec(ws.Recommendations, "no_pg_stat_statements_info"); rec == nil {
		t.Errorf("PG13 + zero StatsSince should fire R8")
	}
}

func TestR8_SilentOnModernServer(t *testing.T) {
	meta := Meta{ServerVersion: "17.2", StatsSince: time.Time{}}
	ws := ComputeFromRows(nil, meta, DefaultOptions())
	if rec := findRec(ws.Recommendations, "no_pg_stat_statements_info"); rec != nil {
		t.Errorf("PG17 should not fire R8 even when StatsSince is zero")
	}
}

func TestRecommendations_SortedBySeverity(t *testing.T) {
	// A workload that triggers R1 (Critical) and R5 (Info). R1 should
	// come first.
	rows := []RawQueryRow{
		{QueryID: 1, Query: "SELECT", Calls: 1, TotalExecTimeMs: 900, MeanExecTimeMs: 900, SharedBlksHit: 9800, SharedBlksRead: 200},
		{QueryID: 2, Query: "SELECT 1", Calls: 500_000, TotalExecTimeMs: 100, MeanExecTimeMs: 0.2},
	}
	ws := ComputeFromRows(rows, Meta{ServerVersion: "17.2"}, DefaultOptions())
	if len(ws.Recommendations) < 2 {
		t.Fatalf("expected >=2 recommendations, got %d", len(ws.Recommendations))
	}
	if ws.Recommendations[0].Severity < ws.Recommendations[len(ws.Recommendations)-1].Severity {
		t.Errorf("recommendations not sorted by severity: %+v", ws.Recommendations)
	}
}
