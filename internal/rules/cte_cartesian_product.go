package rules

import (
	"fmt"

	"github.com/byksy/dbagent/internal/plan"
)

// CTE-scan volume thresholds. A CTE rescanned a handful of times is
// normal; only flag when the cumulative row work crosses into
// territory where rewriting to a JOIN clearly wins.
const (
	cteCartesianMinLoops       = 10
	cteCartesianMinTotalRows   = 10_000
	cteCartesianCriticalRows   = 100_000
)

// CTECartesianProduct flags CTE Scan nodes that are rescanned many
// times with significant row volume. PostgreSQL 11 and earlier always
// materialised the CTE once; 12+ can inline non-recursive CTEs, but
// the inlining is conservative. A CTE used as an inner side of a
// nested loop still reruns per outer row.
type CTECartesianProduct struct{}

func (*CTECartesianProduct) ID() string         { return "cte_cartesian_product" }
func (*CTECartesianProduct) Name() string       { return "CTE rescanned many times" }
func (*CTECartesianProduct) Category() Category { return CategoryPrescriptive }

func (r *CTECartesianProduct) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Plan == nil || ctx.Plan.Root == nil {
		return nil
	}
	var out []Finding
	for _, n := range ctx.Plan.AllNodes() {
		if n.NeverExecuted || n.NodeType != plan.NodeTypeCTEScan {
			continue
		}
		if n.Loops < cteCartesianMinLoops {
			continue
		}
		totalRows := n.Loops * (n.ActualRows + n.RowsRemovedByFilter)
		if totalRows < cteCartesianMinTotalRows {
			continue
		}

		sev := SeverityWarning
		if totalRows >= cteCartesianCriticalRows {
			sev = SeverityCritical
		}

		name := cteDisplayName(n)
		msg := fmt.Sprintf("CTE %q was scanned %d times, processing %d rows cumulatively. Converting to a JOIN or subquery typically avoids repeated work.",
			name, n.Loops, totalRows)

		out = append(out, newFinding(r, n.ID, sev, msg, map[string]any{
			"cte_name":             name,
			"loops":                n.Loops,
			"rows_per_loop":        n.ActualRows,
			"rows_removed_per_loop": n.RowsRemovedByFilter,
			"total_rows_processed": totalRows,
		}))
	}
	return out
}

// cteDisplayName returns the most informative name available for a
// CTE Scan node: the CTE's declared name first, then the query
// alias, then a generic placeholder.
func cteDisplayName(n *plan.Node) string {
	if n.CTEName != "" {
		return n.CTEName
	}
	if n.Alias != "" {
		return n.Alias
	}
	return "<unnamed>"
}
