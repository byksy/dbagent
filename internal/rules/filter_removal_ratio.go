package rules

import (
	"fmt"

	"github.com/byksy/dbagent/internal/plan"
)

// Filter-removal thresholds. The two-factor check (ratio AND row
// count) is what keeps this rule from firing on tiny tables where
// discarding 99% of 4 rows doesn't matter.
const (
	filterRemovalInfoRatio     = 0.50
	filterRemovalWarningRatio  = 0.80
	filterRemovalCriticalRatio = 0.95

	filterRemovalMinRows      = 100
	filterRemovalCriticalRows = 1000
)

// FilterRemovalRatio flags scans that discard the bulk of the rows
// they read. The corresponding prescriptive rule is
// MissingIndexOnFilter.
type FilterRemovalRatio struct{}

func (*FilterRemovalRatio) ID() string         { return "filter_removal_ratio" }
func (*FilterRemovalRatio) Name() string       { return "High filter removal ratio" }
func (*FilterRemovalRatio) Category() Category { return CategoryDiagnostic }

func (r *FilterRemovalRatio) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Plan == nil || ctx.Plan.Root == nil {
		return nil
	}
	p := ctx.Plan
	var out []Finding
	for _, n := range p.AllNodes() {
		if n.NeverExecuted || !isFilterableScan(n.NodeType) {
			continue
		}
		// PostgreSQL reports both Actual Rows and Rows Removed by
		// Filter on a per-loop basis (per-worker-average for parallel
		// scans). Scale both by Loops so the ratio and the volume
		// gates reflect the real work done, not one invocation.
		loops := n.Loops
		if loops < 1 {
			loops = 1
		}
		removedTotal := n.RowsRemovedByFilter * loops
		keptTotal := n.ActualRows * loops
		if removedTotal < filterRemovalMinRows {
			continue
		}
		total := keptTotal + removedTotal
		if total == 0 {
			continue
		}
		ratio := float64(removedTotal) / float64(total)

		var sev Severity
		switch {
		case ratio >= filterRemovalCriticalRatio && removedTotal >= filterRemovalCriticalRows:
			sev = SeverityCritical
		case ratio >= filterRemovalWarningRatio:
			sev = SeverityWarning
		case ratio >= filterRemovalInfoRatio:
			sev = SeverityInfo
		default:
			continue
		}

		msg := fmt.Sprintf("%.0f%% of rows read are discarded by filter (%d rows removed, %d kept).",
			ratio*100, removedTotal, keptTotal)
		out = append(out, newFinding(r, n.ID, sev, msg, map[string]any{
			"rows_removed":  removedTotal,
			"rows_kept":     keptTotal,
			"removal_ratio": ratio,
			"filter":        n.Filter,
		}))
	}
	return out
}

// isFilterableScan reports whether a node is a scan type that can
// carry a "Filter" clause (vs. Index Cond or Hash Cond). Excluding
// non-scan types here keeps the rule focused on the place users
// actually tune: the scan boundary.
func isFilterableScan(t plan.NodeType) bool {
	switch t {
	case plan.NodeTypeSeqScan,
		plan.NodeTypeIndexScan,
		plan.NodeTypeIndexOnlyScan,
		plan.NodeTypeBitmapHeapScan:
		return true
	}
	return false
}
