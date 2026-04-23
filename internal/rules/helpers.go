package rules

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/byksy/dbagent/internal/plan"
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

// predicateRE matches a single column-name-on-the-left predicate. The
// named group "col" is the column. We allow optional surrounding
// parens and an optional ::type cast after the column. The operator
// list covers comparisons, IS [NOT] NULL, IN/ANY, and array ops.
var predicateRE = regexp.MustCompile(`(?i)` +
	`\(*\s*` + // optional leading parens/space
	`(?:lower|upper|trim|btrim|length)?` + // common wrapping funcs with single arg
	`\(*\s*` +
	`(?:"?(?P<col>[A-Za-z_][A-Za-z0-9_]*)"?)` + // column identifier (quoted or not)
	`\s*\)*` + // close wrapping paren
	`(?:::[A-Za-z_][A-Za-z0-9_]*(?:\[\])?)?` + // optional ::type
	`\s*\)*\s*` + // close outer paren
	`(?:=|<|>|<=|>=|<>|!=|IS\s+NULL|IS\s+NOT\s+NULL|IN\s|=\s*ANY|@>|<@|\|\||@@)`)

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
