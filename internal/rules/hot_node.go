package rules

import "fmt"

// Hot-node thresholds. Percentages below 30% are noise — every plan
// has at least one node consuming "a notable chunk" of time, and
// warning on them would make every output noisy. Above 90% is the
// classic single-hot-spot query that dominates all other tuning.
const (
	hotNodeInfoPct     = 0.30
	hotNodeWarningPct  = 0.50
	hotNodeCriticalPct = 0.90

	// hotNodeMinTotalMs: a sub-10ms plan has no meaningful "hot
	// node" — calling anything hot there produces noise on unit
	// tests, health checks, and trivial lookups.
	hotNodeMinTotalMs = 10.0
)

// HotNode flags nodes whose exclusive time dominates the query. It's
// purely diagnostic: it tells the user "this is where the time goes"
// without suggesting a specific fix.
type HotNode struct{}

func (*HotNode) ID() string         { return "hot_node" }
func (*HotNode) Name() string       { return "Hot node" }
func (*HotNode) Category() Category { return CategoryDiagnostic }

func (r *HotNode) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Plan == nil {
		return nil
	}
	p := ctx.Plan
	if p.Root == nil || p.TotalTimeMs <= 0 {
		return nil
	}
	if p.TotalTimeMs < hotNodeMinTotalMs {
		return nil
	}
	var out []Finding
	for _, n := range p.AllNodes() {
		if n.NeverExecuted {
			continue
		}
		excl := n.ExclusiveTimeMs()
		pct := excl / p.TotalTimeMs
		var sev Severity
		switch {
		case pct >= hotNodeCriticalPct:
			sev = SeverityCritical
		case pct >= hotNodeWarningPct:
			sev = SeverityWarning
		case pct >= hotNodeInfoPct:
			sev = SeverityInfo
		default:
			continue
		}
		msg := fmt.Sprintf("This node accounts for %.0f%% of total query time (%.1fms of %.1fms).",
			pct*100, excl, p.TotalTimeMs)
		out = append(out, newFinding(r, n.ID, sev, msg, map[string]any{
			"exclusive_ms":  excl,
			"total_ms":      p.TotalTimeMs,
			"exclusive_pct": pct,
		}))
	}
	return out
}
