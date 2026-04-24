package stats

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/byksy/dbagent/internal/pgstat"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Compute pulls pg_stat_statements data live and returns a fully
// populated WorkloadStats. It composes pgstat.FetchWorkload
// (fetches raw rows) and ComputeFromRows (aggregates and ranks) so
// tests can exercise the math without a live database.
func Compute(ctx context.Context, pool *pgxpool.Pool, opts Options) (*WorkloadStats, error) {
	raw, wmeta, err := pgstat.FetchWorkload(ctx, pool, pgstat.WorkloadOptions{
		SinceMinutes: opts.SinceMinutes,
	})
	if err != nil {
		return nil, err
	}
	meta := Meta{
		Database:      wmeta.Database,
		ServerVersion: wmeta.ServerVersion,
		SnapshotAt:    wmeta.SnapshotAt,
		StatsSince:    wmeta.StatsSince,
	}
	return ComputeFromRows(raw, meta, opts), nil
}

// ComputeFromRows is the pure-function core of Compute. It takes
// already-fetched rows plus Meta and returns the snapshot. Safe to
// call with a nil or empty row slice.
func ComputeFromRows(rows []RawQueryRow, meta Meta, opts Options) *WorkloadStats {
	if opts.TopN <= 0 {
		opts.TopN = DefaultOptions().TopN
	}

	filtered := filterExcluded(rows, opts.ExcludeRegexp)

	ws := &WorkloadStats{
		Meta:            meta,
		Recommendations: []Recommendation{},
	}
	ws.Meta.SchemaVersion = SchemaVersion

	if len(filtered) == 0 {
		// Empty workloads are legitimate (freshly-reset stats, for
		// example). Fire the R7 recommendation later rather than
		// returning an error.
		ws.Recommendations = recommendationsFor(ws, filtered, time.Now())
		return ws
	}

	for _, r := range filtered {
		ws.TotalExecutions += r.Calls
		ws.TotalTimeMs += r.TotalExecTimeMs
		switch classifyQuery(r.Query) {
		case "read":
			ws.ReadTimeMs += r.TotalExecTimeMs
		case "write":
			ws.WriteTimeMs += r.TotalExecTimeMs
		}
	}
	ws.TotalQueries = int64(len(filtered))
	ws.CacheHitRatio = overallCacheHitRatio(filtered)

	// Build per-dimension top-lists. Each list is a fresh slice
	// sorted by a different metric so we can rank independently.
	ws.TopByTotalTime = buildTop(filtered, byTotalTime, opts.TopN, ws.TotalTimeMs)
	ws.TopByCallCount = buildTop(filtered, byCalls, opts.TopN, ws.TotalTimeMs)
	ws.TopByMeanTime = buildTop(filtered, byMeanTime, opts.TopN, ws.TotalTimeMs)
	ws.TopByIO = buildTop(filtered, byIOReads, opts.TopN, ws.TotalTimeMs)
	ws.TopByLowCache = buildTopLowCache(filtered, opts.TopN, ws.TotalTimeMs)

	ws.Recommendations = recommendationsFor(ws, filtered, time.Now())
	return ws
}

// filterExcluded drops rows whose query text matches any of the
// user-supplied regex patterns. Invalid patterns are silently
// ignored so a typo in --exclude doesn't break the whole command.
func filterExcluded(rows []RawQueryRow, patterns []string) []RawQueryRow {
	if len(patterns) == 0 {
		return rows
	}
	var compiled []*regexp.Regexp
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		compiled = append(compiled, re)
	}
	if len(compiled) == 0 {
		return rows
	}
	out := make([]RawQueryRow, 0, len(rows))
rowLoop:
	for _, r := range rows {
		for _, re := range compiled {
			if re.MatchString(r.Query) {
				continue rowLoop
			}
		}
		out = append(out, r)
	}
	return out
}

// overallCacheHitRatio returns the workload-wide hit rate across all
// shared buffer accesses. Queries with no buffer counters (rare —
// DDL, for instance) don't drag the ratio down.
func overallCacheHitRatio(rows []RawQueryRow) float64 {
	var hit, read int64
	for _, r := range rows {
		hit += r.SharedBlksHit
		read += r.SharedBlksRead
	}
	if hit+read == 0 {
		return -1
	}
	return float64(hit) / float64(hit+read)
}

// sortKind is the ranking dimension for buildTop.
type sortKind int

const (
	byTotalTime sortKind = iota
	byCalls
	byMeanTime
	byIOReads
)

// buildTop produces a sorted, rank-annotated top-N list from rows.
// totalTime is the workload total used to compute each entry's
// pct_of_total share.
func buildTop(rows []RawQueryRow, kind sortKind, topN int, totalTime float64) []QueryGroup {
	sorted := make([]RawQueryRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		switch kind {
		case byCalls:
			return a.Calls > b.Calls
		case byMeanTime:
			return a.MeanExecTimeMs > b.MeanExecTimeMs
		case byIOReads:
			return a.SharedBlksRead > b.SharedBlksRead
		}
		return a.TotalExecTimeMs > b.TotalExecTimeMs
	})
	if topN > len(sorted) {
		topN = len(sorted)
	}
	out := make([]QueryGroup, 0, topN)
	for i := 0; i < topN; i++ {
		out = append(out, toQueryGroup(sorted[i], i+1, totalTime))
	}
	return out
}

// buildTopLowCache ranks queries by worst cache hit ratio, but only
// among those with at least one shared buffer access — entries with
// CacheHitRatio == -1 would otherwise sort to the top as "worst".
func buildTopLowCache(rows []RawQueryRow, topN int, totalTime float64) []QueryGroup {
	eligible := make([]RawQueryRow, 0, len(rows))
	for _, r := range rows {
		if r.SharedBlksHit+r.SharedBlksRead > 0 {
			eligible = append(eligible, r)
		}
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		a := queryCacheHit(eligible[i])
		b := queryCacheHit(eligible[j])
		return a < b
	})
	if topN > len(eligible) {
		topN = len(eligible)
	}
	out := make([]QueryGroup, 0, topN)
	for i := 0; i < topN; i++ {
		out = append(out, toQueryGroup(eligible[i], i+1, totalTime))
	}
	return out
}

// queryCacheHit returns a single row's cache hit ratio, or 1.0 (the
// best possible) when there's no buffer data so such rows sort to
// the *bottom* of the "worst cache" list rather than the top.
func queryCacheHit(r RawQueryRow) float64 {
	denom := r.SharedBlksHit + r.SharedBlksRead
	if denom == 0 {
		return 1
	}
	return float64(r.SharedBlksHit) / float64(denom)
}

// toQueryGroup converts a RawQueryRow into the public QueryGroup
// shape, filling in rank, pct_of_total, cache-hit ratio, and the
// query-kind classification.
func toQueryGroup(r RawQueryRow, rank int, totalTime float64) QueryGroup {
	hitRatio := queryCacheHit(r)
	if r.SharedBlksHit+r.SharedBlksRead == 0 {
		hitRatio = -1
	}
	pct := 0.0
	if totalTime > 0 {
		pct = r.TotalExecTimeMs / totalTime
	}
	return QueryGroup{
		Rank:           rank,
		QueryID:        r.QueryID,
		Calls:          r.Calls,
		TotalTimeMs:    r.TotalExecTimeMs,
		MeanTimeMs:     r.MeanExecTimeMs,
		Rows:           r.Rows,
		SharedBlksHit:  r.SharedBlksHit,
		SharedBlksRead: r.SharedBlksRead,
		CacheHitRatio:  hitRatio,
		PctOfTotal:     pct,
		QueryText:      normaliseWhitespace(r.Query),
		QueryKind:      classifyQuery(r.Query),
	}
}

// classifyQuery is a tiny heuristic that looks at the first
// non-whitespace keyword. Good enough for workload-level split
// metrics; rule-level analysis doesn't use it.
func classifyQuery(q string) string {
	trimmed := strings.TrimSpace(strings.ToUpper(q))
	// Strip leading comments / parens so "/* foo */ SELECT ..."
	// still classifies correctly.
	trimmed = strings.TrimLeft(trimmed, "(")
	trimmed = strings.TrimSpace(trimmed)
	switch {
	case strings.HasPrefix(trimmed, "SELECT"), strings.HasPrefix(trimmed, "WITH"):
		return "read"
	case strings.HasPrefix(trimmed, "INSERT"),
		strings.HasPrefix(trimmed, "UPDATE"),
		strings.HasPrefix(trimmed, "DELETE"),
		strings.HasPrefix(trimmed, "MERGE"),
		strings.HasPrefix(trimmed, "UPSERT"):
		return "write"
	case strings.HasPrefix(trimmed, "CREATE"),
		strings.HasPrefix(trimmed, "ALTER"),
		strings.HasPrefix(trimmed, "DROP"),
		strings.HasPrefix(trimmed, "TRUNCATE"),
		strings.HasPrefix(trimmed, "COMMENT"):
		return "ddl"
	}
	return "other"
}

// normaliseWhitespace collapses any run of whitespace to a single
// space so query text renders predictably in tables.
func normaliseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r':
			if !inSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			inSpace = true
		default:
			b.WriteRune(r)
			inSpace = false
		}
	}
	return strings.TrimRight(b.String(), " ")
}
