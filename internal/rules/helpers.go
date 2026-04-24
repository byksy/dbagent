package rules

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/byksy/dbagent/internal/plan"
	"github.com/byksy/dbagent/internal/schema"
)

// ExtractFilterColumns pulls column names from a PostgreSQL filter or
// index-cond expression. The goal is not perfect parsing — it's
// "extract confidently or extract nothing". A wrong Suggested line
// ("CREATE INDEX on the wrong column") costs more user trust than no
// suggestion at all.
//
// Recognised forms:
//   col = X            col > X            col IN (...)
//   col IS NULL        col IS NOT NULL    col @> X        col <@ X
//   col = ANY (...)    col::type = X      lower(col) = X  (extracts col)
//
// Multiple predicates joined by AND/OR are each inspected; the
// returned slice is the set union (de-duplicated, source-order).
// Anything that doesn't match the recognised patterns is silently
// dropped so the caller receives a shorter list or an empty slice.
func ExtractFilterColumns(filter string) []string {
	if filter == "" {
		return []string{}
	}
	// Split into predicates on top-level AND/OR. We don't need true
	// parentheses-aware splitting because each predicate is already
	// parenthesised by PostgreSQL's output; splitting on ` AND ` and
	// ` OR ` (with spaces) is sufficient and keeps the implementation
	// trivial.
	parts := splitPredicates(filter)

	var cols []string
	seen := make(map[string]bool)
	for _, p := range parts {
		col := extractOneColumn(p)
		if col == "" || seen[col] {
			continue
		}
		seen[col] = true
		cols = append(cols, col)
	}
	if cols == nil {
		return []string{}
	}
	return cols
}

// splitPredicates chops a filter string into top-level conjuncts.
// PostgreSQL wraps each predicate in parens, so a naive split on
// " AND " / " OR " lands cleanly between them.
func splitPredicates(filter string) []string {
	s := strings.ReplaceAll(filter, " AND ", "\x01")
	s = strings.ReplaceAll(s, " OR ", "\x01")
	return strings.Split(s, "\x01")
}

// predicateRE matches a single direct column-name-on-the-left
// predicate. Function calls such as lower(col) are handled separately
// by singleArgCallRE — we do NOT try to optionally match a function
// prefix here because that misfires on column names that happen to
// start with a recognised function word (e.g., "lower_col" would be
// split into "lower" + "_col").
var predicateRE = regexp.MustCompile(`(?i)` +
	`\(*\s*` + // optional leading parens/space
	`(?:"?(?P<col>[A-Za-z_][A-Za-z0-9_]*)"?)` + // column identifier (quoted or not)
	`\s*\)*` + // close wrapping paren
	`(?:::[A-Za-z_][A-Za-z0-9_]*(?:\[\])?)?` + // optional ::type
	`\s*\)*\s*` + // close outer paren
	`(?:IS\s+NOT\s+NULL|IS\s+NULL|<=|>=|<>|!=|=\s*ANY|=|<|>|IN\s|@>|<@|!~~\*|!~~|~~\*|~~|\|\||@@)`)

// singleArgCallRE detects "func(col)" with exactly one identifier
// argument so we can still pluck the column out. Two-arg function
// calls like complex_function(a, b) must NOT match — we refuse to
// guess which argument is the one being filtered on.
var singleArgCallRE = regexp.MustCompile(`(?i)^\s*\(*\s*[a-z_][a-z0-9_]*\(\s*\(*\s*"?([a-z_][a-z0-9_]*)"?\s*\)*\s*(?:::[a-z_][a-z0-9_]*)?\s*\)`)

// multiArgCallRE is the sentinel for "we saw a function call with ≥2
// arguments — abandon extraction for this predicate".
var multiArgCallRE = regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_]*\(\s*[^()]*,\s*[^()]*\)`)

// extractOneColumn picks the column reference from a single predicate.
// Returns "" when the predicate is not one of the recognised shapes.
func extractOneColumn(predicate string) string {
	if multiArgCallRE.MatchString(predicate) {
		return ""
	}
	if m := singleArgCallRE.FindStringSubmatch(predicate); m != nil {
		return m[1]
	}
	m := predicateRE.FindStringSubmatch(predicate)
	if m == nil {
		return ""
	}
	for i, name := range predicateRE.SubexpNames() {
		if name == "col" {
			return m[i]
		}
	}
	return ""
}

// FirstRelationName walks from n up through its ancestors and returns
// the first non-empty RelationName it finds. This lets rules on
// BitmapIndexScan (which never names a relation directly — the name
// lives on the parent BitmapHeapScan) still answer "which table are
// we scanning?"
func FirstRelationName(n *plan.Node) string {
	for cur := n; cur != nil; cur = cur.Parent {
		if cur.RelationName != "" {
			return cur.RelationName
		}
	}
	return ""
}

// FormatWorkMem converts a kilobyte count into a work_mem setting
// string PostgreSQL will accept. Rounds up to the next power-of-2
// megabyte value, capped at 2GB (a reasonable ceiling for a single
// query's allotment).
func FormatWorkMem(kb int64) string {
	if kb <= 0 {
		return "4MB"
	}
	// Round up to MB.
	mb := (kb + 1023) / 1024
	// Round up to the next power of 2.
	p := int64(1)
	for p < mb {
		p *= 2
	}
	if p >= 1024 {
		gb := p / 1024
		if gb > 2 {
			gb = 2
		}
		return fmt.Sprintf("%dGB", gb)
	}
	return fmt.Sprintf("%dMB", p)
}

// mulSaturating multiplies two non-negative int64 values with
// saturating semantics: if the true product would overflow int64,
// it returns math.MaxInt64 instead of wrapping to a negative
// number. Used by rules that multiply row counts by byte widths,
// where overflow on pathological inputs would otherwise make the
// severity gates fire in the wrong direction.
func mulSaturating(a, b int64) int64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > math.MaxInt64/b {
		return math.MaxInt64
	}
	return a * b
}

// humanBytes formats a byte count for human display. Anything under
// 1024 is shown as bytes with no suffix decoration; kB/MB/GB values
// carry one decimal so "7,340,032" reads as "7.0 MB" rather than
// "7000.0 kB".
func humanBytes(n int64) string {
	const (
		kB = 1024
		mB = 1024 * kB
		gB = 1024 * mB
	)
	switch {
	case n < 0:
		return "-" + humanBytes(-n)
	case n >= gB:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gB))
	case n >= mB:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mB))
	case n >= kB:
		return fmt.Sprintf("%.1f kB", float64(n)/float64(kB))
	}
	return fmt.Sprintf("%d B", n)
}

// bloatFactor returns an approximate ratio of bytes-read to the
// minimum-expected bytes the row work would justify. Used by the
// table_bloat rule as a cheap, conservative indicator that the
// scan moved far more data than its returned rows warrant. Returns
// 0 when either the block count or the row-work estimate is zero —
// callers should treat that as "no signal", not "no bloat".
//
// The 8-bytes-per-row lower bound is intentionally tiny; the goal
// is to catch scans that are pulling whole pages for almost-empty
// rows (classic dead-tuple bloat).
func bloatFactor(blocks, rows, width int64) float64 {
	if blocks <= 0 || rows <= 0 {
		return 0
	}
	const (
		bytesPerBlock  = 8192
		minBytesPerRow = 8
	)
	bytesRead := float64(blocks) * bytesPerBlock
	expected := float64(rows) * float64(width)
	if expected < float64(rows*minBytesPerRow) {
		expected = float64(rows * minBytesPerRow)
	}
	if expected == 0 {
		return 0
	}
	return bytesRead / expected
}

// collectTouchedTables walks the plan and returns the first scan
// node's ID for each base relation it encounters. Keys are fully
// qualified names ("schema.relation"); values are node IDs suitable
// for attaching findings.
//
// NeverExecuted scans are skipped: anchoring a finding to a branch
// the plan never actually entered would mislead the operator about
// which relation this query hit. Shared across schema-aware rules
// (fk_missing_index, unused_index_hint, duplicate_index, table_bloat).
func collectTouchedTables(p *plan.Plan) map[string]int {
	out := map[string]int{}
	if p == nil || p.Root == nil {
		return out
	}
	p.Root.Walk(func(n *plan.Node) {
		if n.NeverExecuted || n.RelationName == "" || !isScanNode(n.NodeType) {
			return
		}
		fqn := fullyQualified(n.Schema, n.RelationName)
		if _, seen := out[fqn]; !seen {
			out[fqn] = n.ID
		}
	})
	return out
}

// fullyQualified builds a Schema-key-compatible FQN for a plan
// node's relation. It defers to schema.Table.FQN so identifier
// quoting stays consistent between plan-derived names (bare, as
// PostgreSQL EXPLAIN emits them) and Schema keys (always qualified,
// with quoting for MixedCase or reserved identifiers). Without this
// normalisation, touched-table matching would silently fail on any
// identifier that needed quoting.
func fullyQualified(s, name string) string {
	if name == "" {
		return ""
	}
	if s == "" {
		s = "public"
	}
	return (&schema.Table{Schema: s, Name: name}).FQN()
}

// findIndexByName returns the *schema.Index from xs whose Name
// matches target, or nil. Used by composite_index_extension and
// similar rules that know an index's name from the plan node and
// need to reach the Schema entry.
func findIndexByName(xs []*schema.Index, target string) *schema.Index {
	for _, idx := range xs {
		if idx != nil && idx.Name == target {
			return idx
		}
	}
	return nil
}

// isScanNode reports whether a NodeType reads rows from a user
// table directly. Non-scan nodes (joins, aggregates) don't have a
// RelationName and shouldn't anchor schema-aware findings.
func isScanNode(t plan.NodeType) bool {
	switch t {
	case plan.NodeTypeSeqScan,
		plan.NodeTypeIndexScan,
		plan.NodeTypeIndexOnlyScan,
		plan.NodeTypeBitmapHeapScan,
		plan.NodeTypeTidScan,
		plan.NodeTypeForeignScan:
		return true
	}
	return false
}
