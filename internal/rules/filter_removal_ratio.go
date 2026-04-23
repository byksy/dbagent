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

func (r *FilterRemovalRatio) Check(p *plan.Plan) []Finding {
	if p == nil || p.Root == nil {
		return nil
	}
	var out []Finding
	for _, n := range p.AllNodes() {
		if n.NeverExecuted || !isFilterableScan(n.NodeType) {
			continue
		}
		removed := n.RowsRemovedByFilter
		if removed < filterRemovalMinRows {
			continue
		}
		kept := n.ActualRows
		if n.Loops > 1 {
			kept = kept * n.Loops
		}
		total := kept + removed
		if total == 0 {
			continue
		}
		ratio := float64(removed) / float64(total)

		var sev Severity
		switch {
		case ratio >= filterRemovalCriticalRatio && removed >= filterRemovalCriticalRows:
			sev = SeverityCritical
		case ratio >= filterRemovalWarningRatio:
			sev = SeverityWarning
		case ratio >= filterRemovalInfoRatio:
			sev = SeverityInfo
		default:
			continue
		}

		msg := fmt.Sprintf("%.0f%% of rows read are discarded by filter (%d rows removed, %d kept).",
			ratio*100, removed, kept)
		out = append(out, newFinding(r, n.ID, sev, msg, map[string]any{
			"rows_removed":  removed,
			"rows_kept":     kept,
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
