package rules

import (
	"fmt"
	"strings"

	"github.com/byksy/dbagent/internal/plan"
)

// Volume thresholds. The 5× factor means "the filter threw away more
// than 80% of what was scanned", keeping the rule from firing on
// well-estimated predicates. The loops*removed gate suppresses the
// rule on tiny tables where a seq scan is fine.
const (
	missingIndexRemovalFactor   = 5
	missingIndexMinWeightedRows = 100
	missingIndexCriticalRows    = 10_000
)

// MissingIndexOnFilter is the prescriptive counterpart to
// FilterRemovalRatio: when a filter is discarding the bulk of a
// scan's rows at meaningful volume, propose an index on the
// filter's columns.
type MissingIndexOnFilter struct{}

func (*MissingIndexOnFilter) ID() string         { return "missing_index_on_filter" }
func (*MissingIndexOnFilter) Name() string       { return "Missing index on filter" }
func (*MissingIndexOnFilter) Category() Category { return CategoryPrescriptive }

func (r *MissingIndexOnFilter) Check(p *plan.Plan) []Finding {
	if p == nil || p.Root == nil {
		return nil
	}
	var out []Finding
	for _, n := range p.AllNodes() {
		if n.NeverExecuted || !missingIndexApplies(n) {
			continue
		}
		// The ratio gate stays on per-loop figures (the filter's
		// selectivity per invocation is what matters for "index would
		// help"), but volume gates and user-facing numbers use
		// loop-scaled totals so parallel / nested-loop inner scans
		// don't under-report.
		removedPerLoop := n.RowsRemovedByFilter
		keptPerLoop := n.ActualRows
		if keptPerLoop*int64(missingIndexRemovalFactor) >= removedPerLoop {
			continue
		}
		loops := n.Loops
		if loops < 1 {
			loops = 1
		}
		removedTotal := removedPerLoop * loops
		keptTotal := keptPerLoop * loops
		if removedTotal < missingIndexMinWeightedRows {
			continue
		}

		relation := FirstRelationName(n)
		cols := ExtractFilterColumns(n.Filter)

		sev := SeverityWarning
		if removedTotal > missingIndexCriticalRows {
			sev = SeverityCritical
		}

		ratio := 1.0
		if keptTotal+removedTotal > 0 {
			ratio = float64(removedTotal) / float64(keptTotal+removedTotal)
		}
		msg := fmt.Sprintf("Filter removes %.0f%% of rows scanned (%d rows). Consider an index to push the predicate down.",
			ratio*100, removedTotal)

		ev := map[string]any{
			"rows_removed":    removedTotal,
			"rows_kept":       keptTotal,
			"loops":           loops,
			"filter":          n.Filter,
			"parsed_columns":  cols,
			"relation":        relation,
		}

		f := newFinding(r, n.ID, sev, msg, ev)

		if relation != "" && len(cols) > 0 {
			f.Suggested = fmt.Sprintf("CREATE INDEX ON %s (%s);",
				qualifiedRelation(n, relation), strings.Join(cols, ", "))
			f.SuggestedMeta = map[string]any{
				"kind":     "create_index",
				"relation": relation,
				"schema":   n.Schema,
				"columns":  cols,
				"where":    "",
				"method":   "btree",
			}
		}
		out = append(out, f)
	}
	return out
}

// missingIndexApplies gates the rule to scans that have a Filter
// clause (not just Index Cond). Index Cond predicates already use
// the index, so suggesting "another index" there would be wrong.
func missingIndexApplies(n *plan.Node) bool {
	if n.Filter == "" {
		return false
	}
	switch n.NodeType {
	case plan.NodeTypeSeqScan,
		plan.NodeTypeBitmapHeapScan,
		plan.NodeTypeIndexScan,
		plan.NodeTypeIndexOnlyScan:
		return true
	}
	return false
}

// qualifiedRelation prepends schema when known. The relation name
// argument is passed separately because FirstRelationName may have
// walked up the tree to find it.
func qualifiedRelation(n *plan.Node, relation string) string {
	if n.Schema != "" {
		return n.Schema + "." + relation
	}
	return relation
}
