package rules

import (
	"fmt"
	"strings"

	"github.com/byksy/dbagent/internal/plan"
)

// CompositeIndexExtension flags Index Scans that carry a trailing
// Filter — i.e., the index covers the condition the planner chose
// to drive the scan, but additional predicates still re-check each
// row after the fetch. Extending the index with the residual filter
// columns often removes the extra work.
//
// This rule intentionally overlaps with missing_index_on_filter:
// that rule fires on Seq / Bitmap Heap scans without any usable
// index; this one targets Index Scans that already have *some*
// index but could use a better one. Users should sort the two as
// complementary, not duplicate.
type CompositeIndexExtension struct{}

func (*CompositeIndexExtension) ID() string         { return "composite_index_extension" }
func (*CompositeIndexExtension) Name() string       { return "Composite index extension" }
func (*CompositeIndexExtension) Category() Category { return CategoryPrescriptive }

func (r *CompositeIndexExtension) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Plan == nil || ctx.Schema == nil {
		return nil
	}
	var out []Finding
	for _, n := range ctx.Plan.AllNodes() {
		if n.NeverExecuted {
			continue
		}
		if !isIndexScan(n.NodeType) {
			continue
		}
		if n.Filter == "" {
			continue
		}
		if n.IndexName == "" || n.RelationName == "" {
			continue
		}
		tableFQN := fullyQualified(n.Schema, n.RelationName)
		existing := findIndexByName(ctx.Schema.IndexesOn(tableFQN), n.IndexName)
		if existing == nil {
			continue
		}
		if existing.IsPrimary || existing.IsUnique || existing.IsPartial {
			continue
		}
		if existing.Method != "" && existing.Method != "btree" {
			continue
		}

		extraCols := ExtractFilterColumns(n.Filter)
		if len(extraCols) == 0 {
			continue
		}
		proposed := mergeColumns(existing.Columns, extraCols)
		if len(proposed) == len(existing.Columns) {
			// Nothing new to add — the filter references columns
			// already in the index. The planner's residual filter is
			// redundant; that's a different (out-of-scope) issue.
			continue
		}

		msg := fmt.Sprintf("Index %q covers the index condition but not the trailing filter. Extending to (%s) would avoid the additional work.",
			existing.Name, strings.Join(proposed, ", "))

		ev := map[string]any{
			"index_name":       existing.Name,
			"current_columns":  existing.Columns,
			"proposed_columns": proposed,
			"trailing_filter":  n.Filter,
		}

		f := newFinding(r, n.ID, SeverityWarning, msg, ev)
		f.Suggested = fmt.Sprintf("-- Existing: %s\nDROP INDEX %s;\nCREATE INDEX ON %s (%s);",
			existing.Definition,
			existing.FQN(),
			tableFQN,
			strings.Join(proposed, ", "),
		)
		f.SuggestedMeta = map[string]any{
			"kind":           "extend_index",
			"existing_index": existing.Name,
			"relation":       n.RelationName,
			"schema":         n.Schema,
			"columns":        proposed,
			"where":          "",
			"method":         "btree",
		}
		out = append(out, f)
	}
	return out
}

// isIndexScan returns true for scan nodes that read through an
// index and can carry a residual filter.
func isIndexScan(t plan.NodeType) bool {
	return t == plan.NodeTypeIndexScan || t == plan.NodeTypeIndexOnlyScan
}

// mergeColumns returns current + any columns from extra that aren't
// already in current, preserving current's ordering. The result is
// a proposed column list for an extended btree index.
func mergeColumns(current, extra []string) []string {
	seen := make(map[string]bool, len(current))
	out := make([]string, 0, len(current)+len(extra))
	for _, c := range current {
		seen[c] = true
		out = append(out, c)
	}
	for _, e := range extra {
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}
