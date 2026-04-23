package rules

import (
	"fmt"

	"github.com/byksy/dbagent/internal/plan"
)

// Misestimate tiers. Postgres's planner routinely mis-estimates by
// 2-5×, so anything under 10× is normal background noise. 10-100× is
// a yellow flag (usually stale statistics). 100-1000× is genuinely
// painful, and > 1000× is almost always a bug in what was ANALYZE'd.
const (
	misestimateInfoFactor     = 10.0
	misestimateWarningFactor  = 100.0
	misestimateCriticalFactor = 1000.0
)

// RowMisestimate flags nodes where the planner's per-loop row
// estimate is dramatically off from actual.
type RowMisestimate struct{}

func (*RowMisestimate) ID() string         { return "row_misestimate" }
func (*RowMisestimate) Name() string       { return "Row misestimate" }
func (*RowMisestimate) Category() Category { return CategoryDiagnostic }

func (r *RowMisestimate) Check(p *plan.Plan) []Finding {
	if p == nil || p.Root == nil {
		return nil
	}
	var out []Finding
	for _, n := range p.AllNodes() {
		if n.NeverExecuted {
			continue
		}
		factor := n.MisestimateFactor()
		if factor < misestimateInfoFactor {
			continue
		}
		var sev Severity
		switch {
		case factor >= misestimateCriticalFactor:
			sev = SeverityCritical
		case factor >= misestimateWarningFactor:
			sev = SeverityWarning
		default:
			sev = SeverityInfo
		}

		dir := "under"
		if n.MisestimateDirection() < 0 {
			dir = "over"
		}

		msg := fmt.Sprintf("Planner estimated %d rows, actual is %d (%.1f× %s).",
			n.PlanRows, n.ActualRows, factor, dir)

		ev := map[string]any{
			"plan_rows":   n.PlanRows,
			"actual_rows": n.ActualRows,
			"factor":      factor,
			"direction":   dir,
		}

		f := newFinding(r, n.ID, sev, msg, ev)

		// Only offer ANALYZE when the node is a base-relation scan;
		// misestimates on joins or aggregates rarely improve from
		// ANALYZE of one relation.
		if n.RelationName != "" && isBaseScan(n.NodeType) {
			f.Suggested = fmt.Sprintf("ANALYZE %s;", qualifiedName(n))
			f.SuggestedMeta = map[string]any{
				"kind":     "analyze",
				"relation": n.RelationName,
				"schema":   n.Schema,
			}
		}
		out = append(out, f)
	}
	return out
}

// isBaseScan reports whether a NodeType directly reads a user table
// or index (as opposed to joins, sorts, aggregates, etc.).
func isBaseScan(t plan.NodeType) bool {
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

// qualifiedName returns "schema.relation" when a schema is known,
// else just the relation name. Bare names are fine for ANALYZE as
// long as the default search_path covers the relation.
func qualifiedName(n *plan.Node) string {
	if n.Schema != "" {
		return n.Schema + "." + n.RelationName
	}
	return n.RelationName
}
