package stats

import (
	"fmt"
	"sort"
	"time"

	"github.com/byksy/dbagent/internal/rules"
)

// Recommendation threshold constants. All live in one block so
// reviewers (and future operators reading docs/stats.md) can see
// the full rule set at a glance.
const (
	// R1 — share of total time
	r1WarningShare  = 0.30
	r1CriticalShare = 0.50

	// R3 — overall cache hit ratio
	r3WarningCache  = 0.90
	r3CriticalCache = 0.70

	// R4 — planning-vs-execution count
	r4MinQueries = 5

	// R5 — frequent trivial queries
	r5MinCalls    = 100_000
	r5MaxMeanTime = 1.0 // ms

	// R6 — per-query low cache hit
	r6MaxCacheHit    = 0.50
	r6MinTotalBlocks = 1_000

	// R7 — stats_since freshness
	r7RecentThreshold = time.Hour
)

// recommendationsFor runs every workload-level check and returns the
// findings sorted by (severity desc, id asc) so callers get the
// most urgent advice first.
func recommendationsFor(ws *WorkloadStats, raw []RawQueryRow, now time.Time) []Recommendation {
	out := []Recommendation{}
	if rec := recR1HighConcentration(ws); rec != nil {
		out = append(out, *rec)
	}
	if rec := recR2WriteHeavy(ws); rec != nil {
		out = append(out, *rec)
	}
	if rec := recR3LowOverallCache(ws); rec != nil {
		out = append(out, *rec)
	}
	if rec := recR6LowPerQueryCache(raw); rec != nil {
		out = append(out, *rec)
	}
	if rec := recR5TrivialFrequent(raw); rec != nil {
		out = append(out, *rec)
	}
	if rec := recR7StatsRecentlyReset(ws, now); rec != nil {
		out = append(out, *rec)
	}
	if rec := recR8NoPgStatInfo(ws); rec != nil {
		out = append(out, *rec)
	}
	// R4 left out of Stage 5.5: pg_stat_statements exposes
	// total_plan_time separately but our RawQueryRow doesn't carry
	// it yet. Placeholder kept so future pulls slot in cleanly.

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// mkRec is a tiny constructor that keeps Severity and SeverityName
// in lockstep — JSON consumers only see the string field, tests
// often branch on the enum.
func mkRec(id string, sev rules.Severity, title, msg string, evidence map[string]any) Recommendation {
	return Recommendation{
		ID:           id,
		Severity:     sev,
		SeverityName: sev.String(),
		Title:        title,
		Message:      msg,
		Evidence:     evidence,
	}
}

// recR1HighConcentration fires when the single hottest query owns a
// dominant share of total DB time. The >50% / 30–50% split matches
// the "one query eats the whole workload" vs "one query stands out"
// distinction users report about their own dbs.
func recR1HighConcentration(ws *WorkloadStats) *Recommendation {
	if len(ws.TopByTotalTime) == 0 || ws.TotalTimeMs <= 0 {
		return nil
	}
	top := ws.TopByTotalTime[0]
	share := top.TotalTimeMs / ws.TotalTimeMs
	if share < r1WarningShare {
		return nil
	}
	sev := rules.SeverityWarning
	if share >= r1CriticalShare {
		sev = rules.SeverityCritical
	}
	rec := mkRec("high_total_time_concentration", sev,
		"Single query dominates DB time",
		fmt.Sprintf("One query accounts for %.0f%% of total database time. Consider deep analysis with 'dbagent analyze'.", share*100),
		map[string]any{
			"query_id":     top.QueryID,
			"share":        share,
			"total_time_ms": top.TotalTimeMs,
		},
	)
	rec.Action = fmt.Sprintf("dbagent analyze --queryid %d  # (capture plan first)", top.QueryID)
	return &rec
}

// recR2WriteHeavy highlights workloads where writes dominate. Not a
// severity signal on its own — just an orientation hint.
func recR2WriteHeavy(ws *WorkloadStats) *Recommendation {
	if ws.TotalTimeMs <= 0 {
		return nil
	}
	share := ws.WriteTimeMs / ws.TotalTimeMs
	if share <= 0.5 {
		return nil
	}
	rec := mkRec("write_heavy_workload", rules.SeverityInfo,
		"Workload is write-dominated",
		fmt.Sprintf("Writes dominate this workload (%.0f%% of time). Check for over-indexing, long transactions, or trigger overhead.", share*100),
		map[string]any{
			"write_share":    share,
			"write_time_ms":  ws.WriteTimeMs,
			"total_time_ms":  ws.TotalTimeMs,
		},
	)
	return &rec
}

// recR3LowOverallCache fires on workloads whose overall cache hit
// ratio lags behind the ~95% expected of well-tuned OLTP databases.
func recR3LowOverallCache(ws *WorkloadStats) *Recommendation {
	if ws.CacheHitRatio < 0 {
		return nil
	}
	if ws.CacheHitRatio >= r3WarningCache {
		return nil
	}
	sev := rules.SeverityWarning
	if ws.CacheHitRatio < r3CriticalCache {
		sev = rules.SeverityCritical
	}
	return &[]Recommendation{mkRec("low_overall_cache_hit", sev,
		"Overall cache hit ratio is below target",
		fmt.Sprintf("Cache hit ratio is %.1f%% — below the 95%% target typical of healthy workloads. Consider increasing shared_buffers or adding indexes.", ws.CacheHitRatio*100),
		map[string]any{
			"cache_hit_ratio": ws.CacheHitRatio,
		},
	)}[0]
}

// recR5TrivialFrequent flags hot, cheap queries — classic
// "N+1 queries from the application" pattern.
func recR5TrivialFrequent(rows []RawQueryRow) *Recommendation {
	var best *RawQueryRow
	for i := range rows {
		r := &rows[i]
		if r.Calls < r5MinCalls || r.MeanExecTimeMs >= r5MaxMeanTime {
			continue
		}
		if best == nil || r.Calls > best.Calls {
			best = r
		}
	}
	if best == nil {
		return nil
	}
	rec := mkRec("very_frequent_trivial_queries", rules.SeverityInfo,
		"High-frequency, low-latency query",
		fmt.Sprintf("Query executed %s times with %.2fms mean. Consider caching, batching, or connection pooling.",
			commas(best.Calls), best.MeanExecTimeMs),
		map[string]any{
			"query_id":        best.QueryID,
			"calls":           best.Calls,
			"mean_exec_time_ms": best.MeanExecTimeMs,
		},
	)
	return &rec
}

// recR6LowPerQueryCache surfaces an individual query whose cache
// hit ratio is very poor. A workload-wide R3 can hide a single hot
// query with the worst ratio; R6 picks it out.
func recR6LowPerQueryCache(rows []RawQueryRow) *Recommendation {
	var worst *RawQueryRow
	var worstRatio float64 = 1
	for i := range rows {
		r := &rows[i]
		total := r.SharedBlksHit + r.SharedBlksRead
		if total < r6MinTotalBlocks {
			continue
		}
		ratio := float64(r.SharedBlksHit) / float64(total)
		if ratio >= r6MaxCacheHit {
			continue
		}
		if worst == nil || ratio < worstRatio {
			worst = r
			worstRatio = ratio
		}
	}
	if worst == nil {
		return nil
	}
	rec := mkRec("query_with_very_low_cache_hit", rules.SeverityWarning,
		"Query with very low cache hit ratio",
		fmt.Sprintf("Query has cache hit ratio of %.1f%%. Either it scans a large cold table or an index is missing.", worstRatio*100),
		map[string]any{
			"query_id":         worst.QueryID,
			"cache_hit_ratio":  worstRatio,
			"shared_blks_hit":  worst.SharedBlksHit,
			"shared_blks_read": worst.SharedBlksRead,
		},
	)
	return &rec
}

// recR7StatsRecentlyReset warns callers when pg_stat_statements was
// reset recently — the other recommendations will look misleadingly
// calm in that window.
func recR7StatsRecentlyReset(ws *WorkloadStats, now time.Time) *Recommendation {
	if ws.Meta.StatsSince.IsZero() {
		return nil
	}
	age := now.Sub(ws.Meta.StatsSince)
	if age >= r7RecentThreshold {
		return nil
	}
	return &[]Recommendation{mkRec("stats_recently_reset", rules.SeverityInfo,
		"pg_stat_statements was reset recently",
		fmt.Sprintf("pg_stat_statements was reset %s ago. Current data may not represent a full workload cycle.",
			age.Round(time.Second)),
		map[string]any{
			"stats_since": ws.Meta.StatsSince,
			"age_seconds": age.Seconds(),
		},
	)}[0]
}

// recR8NoPgStatInfo alerts users on older PostgreSQL where the
// stats-reset timestamp is unavailable. FetchWorkload signals this
// by leaving StatsSince zero when the info view is missing, but
// explicitly recording the server version lets us be precise.
func recR8NoPgStatInfo(ws *WorkloadStats) *Recommendation {
	if !ws.Meta.StatsSince.IsZero() {
		return nil
	}
	if ws.Meta.ServerVersion == "" {
		return nil
	}
	// Don't bother firing if the server is recent enough to have
	// the view — StatsSince being zero then usually means the
	// integration caller didn't fill it, which is a different
	// concern we don't address here.
	if majorAtLeast(ws.Meta.ServerVersion, 14) {
		return nil
	}
	return &[]Recommendation{mkRec("no_pg_stat_statements_info", rules.SeverityInfo,
		"pg_stat_statements_info not available",
		fmt.Sprintf("Running on PostgreSQL %s, which doesn't expose pg_stat_statements_info. Stats reset time unknown; interpret accordingly.", ws.Meta.ServerVersion),
		map[string]any{
			"server_version": ws.Meta.ServerVersion,
		},
	)}[0]
}

// majorAtLeast extracts the major version number from a "17.2"-ish
// server_version string and reports whether it meets min. "server_version"
// can trail platform info ("17.2 (Debian …)") which is why we can't
// just parse with strconv.Atoi.
func majorAtLeast(version string, min int) bool {
	var major int
	for i := 0; i < len(version); i++ {
		c := version[i]
		if c >= '0' && c <= '9' {
			major = major*10 + int(c-'0')
			continue
		}
		break
	}
	return major >= min
}

// commas inserts thousands separators into a non-negative int.
func commas(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprint(n)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	first := len(s) % 3
	var out string
	if first > 0 {
		out = s[:first]
	}
	for i := first; i < len(s); i += 3 {
		if out != "" {
			out += ","
		}
		out += s[i : i+3]
	}
	if neg {
		return "-" + out
	}
	return out
}
