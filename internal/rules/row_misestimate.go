package rules

import (
	"fmt"
	"time"

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

	// Per-node noise gate: a node that ran for less than 1ms AND
	// produced fewer than 100 rows is too small to matter even with
	// a big percentage misestimate. This keeps trivial lookups from
	// generating findings.
	misestimateMinExclusiveMs = 1.0
	misestimateMinRowsTotal   = 100

	// Stale-analyze bump: when schema is loaded and the table's last
	// ANALYZE is older than this, escalate severity one tier. Makes
	// "misestimate + stale stats" easy to spot at a glance.
	misestimateStaleAnalyzeAge = 7 * 24 * time.Hour
)

// RowMisestimate flags nodes where the planner's per-loop row
// estimate is dramatically off from actual.
type RowMisestimate struct{}

func (*RowMisestimate) ID() string         { return "row_misestimate" }
func (*RowMisestimate) Name() string       { return "Row misestimate" }
func (*RowMisestimate) Category() Category { return CategoryDiagnostic }

func (r *RowMisestimate) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Plan == nil || ctx.Plan.Root == nil {
		return nil
	}
	p := ctx.Plan
	var out []Finding
	for _, n := range p.AllNodes() {
		if n.NeverExecuted {
			continue
		}
		// Noise gate: tiny nodes aren't worth flagging even at a
		// large factor — the absolute impact is negligible.
		if n.ExclusiveTimeMs() < misestimateMinExclusiveMs && n.ActualRowsTotal() < misestimateMinRowsTotal {
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

		// When schema is loaded, annotate with last-analyze age and
		// bump severity a tier if the table hasn't been ANALYZEd in
		// a week. This makes the common root-cause (stale stats)
		// jump off the page.
		if ctx.Schema != nil && n.RelationName != "" && isBaseScan(n.NodeType) {
			if table := ctx.Schema.FindTable(qualifiedName(n)); table != nil && !table.LastAnalyzed.IsZero() {
				age := time.Since(table.LastAnalyzed)
				ev["last_analyzed"] = table.LastAnalyzed.Format(time.RFC3339)
				ev["analyze_age_hours"] = age.Hours()
				if age > misestimateStaleAnalyzeAge {
					if sev < SeverityCritical {
						sev++
					}
					days := int(age.Hours() / 24)
					msg += fmt.Sprintf(" Last ANALYZE was %d days ago.", days)
				}
			}
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
