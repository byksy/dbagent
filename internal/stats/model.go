// Package stats produces a workload-level snapshot of a PostgreSQL
// database derived from pg_stat_statements. It complements the
// per-plan `analyze` command: where `analyze` asks "why is this one
// query slow?", stats asks "where is the time going across the
// whole database?"
//
// All types here are serialisable to the JSON shape documented in
// schemas/stats-v1.json. Field names are frozen for v1; renaming
// requires a new schema version.
package stats

import (
	"time"

	"github.com/byksy/dbagent/internal/pgstat"
	"github.com/byksy/dbagent/internal/rules"
)

// SchemaVersion tags every emitted WorkloadStats so consumers can
// reject incompatible versions without attempting to parse them.
const SchemaVersion = "stats-v1"

// WorkloadStats is the top-level snapshot returned by Compute.
type WorkloadStats struct {
	Meta Meta `json:"meta"`

	TotalQueries    int64   `json:"total_queries"`
	TotalExecutions int64   `json:"total_executions"`
	TotalTimeMs     float64 `json:"total_time_ms"`
	ReadTimeMs      float64 `json:"read_time_ms"`
	WriteTimeMs     float64 `json:"write_time_ms"`
	CacheHitRatio   float64 `json:"cache_hit_ratio"`

	TopByTotalTime []QueryGroup `json:"top_by_total_time"`
	TopByCallCount []QueryGroup `json:"top_by_call_count"`
	TopByMeanTime  []QueryGroup `json:"top_by_mean_time"`
	TopByIO        []QueryGroup `json:"top_by_io"`
	TopByLowCache  []QueryGroup `json:"top_by_low_cache"`

	Recommendations []Recommendation `json:"recommendations"`
}

// Meta records provenance for a workload snapshot.
type Meta struct {
	Database       string    `json:"database"`
	ServerVersion  string    `json:"server_version"`
	SnapshotAt     time.Time `json:"snapshot_at"`
	StatsSince     time.Time `json:"stats_since,omitempty"`
	DBAgentVersion string    `json:"dbagent_version"`
	SchemaVersion  string    `json:"schema_version"`
}

// QueryGroup is one query's contribution to a workload, ranked
// within a particular top-list.
type QueryGroup struct {
	Rank           int     `json:"rank"`
	QueryID        int64   `json:"query_id"`
	Calls          int64   `json:"calls"`
	TotalTimeMs    float64 `json:"total_time_ms"`
	MeanTimeMs     float64 `json:"mean_time_ms"`
	Rows           int64   `json:"rows"`
	SharedBlksHit  int64   `json:"shared_blks_hit"`
	SharedBlksRead int64   `json:"shared_blks_read"`

	// CacheHitRatio is -1 when neither hit nor read counters are set
	// (for example on DDL that doesn't touch shared buffers).
	CacheHitRatio float64 `json:"cache_hit_ratio"`

	PctOfTotal float64 `json:"pct_of_total"`
	QueryText  string  `json:"query_text"`
	QueryKind  string  `json:"query_kind"`
}

// Recommendation is a workload-level observation distinct from a
// plan-level rule finding. Rules analyse plans; recommendations
// analyse aggregated workload metrics.
type Recommendation struct {
	ID       string         `json:"id"`
	Severity rules.Severity `json:"-"`

	// SeverityName exists purely for JSON — rules.Severity is an int
	// enum and serialising the integer leaks implementation detail.
	SeverityName string         `json:"severity"`
	Title        string         `json:"title"`
	Message      string         `json:"message"`
	Evidence     map[string]any `json:"evidence,omitempty"`
	Action       string         `json:"action,omitempty"`
}

// RawQueryRow is the shape Compute consumes. Aliased to
// pgstat.WorkloadRow so the fetcher and aggregator share exactly
// one canonical row type — no translation step, no drift — while
// still letting the stats package keep a clean top-level API.
type RawQueryRow = pgstat.WorkloadRow

// Options tunes the Compute / ComputeFromRows behaviour.
type Options struct {
	TopN int
	// SinceMinutes is accepted for forward compatibility but is not
	// currently applied as a rolling filter — pg_stat_statements has
	// no per-statement timestamp. It is propagated to the fetcher so
	// a real filter can be added without touching callers.
	SinceMinutes  int
	ExcludeRegexp []string // skip queries whose text matches any of these patterns
	IncludeSystem bool     // disable the default noise filter (pg_catalog, SET/SHOW, VACUUM, ...)
}

// DefaultOptions returns the default tuning (TopN=10, no filters).
func DefaultOptions() Options {
	return Options{TopN: 10}
}
