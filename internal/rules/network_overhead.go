package rules

import "fmt"

// Byte thresholds for network-overhead tiers. 10 MB is the floor
// where a single query's payload starts showing up in application
// latency budgets; 1 GB is unambiguously a mistake for almost every
// workload.
const (
	networkOverheadInfoBytes     int64 = 10 * 1024 * 1024
	networkOverheadWarningBytes  int64 = 100 * 1024 * 1024
	networkOverheadCriticalBytes int64 = 1024 * 1024 * 1024
)

// NetworkOverhead flags queries that return a lot of data to the
// client. It fires on the plan root only because downstream nodes
// project narrower tuples — the shape the client sees is what the
// root emits.
type NetworkOverhead struct{}

func (*NetworkOverhead) ID() string         { return "network_overhead" }
func (*NetworkOverhead) Name() string       { return "Large result set over the wire" }
func (*NetworkOverhead) Category() Category { return CategoryDiagnostic }

func (r *NetworkOverhead) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Plan == nil || ctx.Plan.Root == nil {
		return nil
	}
	root := ctx.Plan.Root
	if root.NeverExecuted {
		return nil
	}
	rows := root.ActualRowsTotal()
	width := root.PlanWidth
	if rows <= 0 || width <= 0 {
		return nil
	}
	totalBytes := mulSaturating(rows, width)
	if totalBytes < networkOverheadInfoBytes {
		return nil
	}

	sev := SeverityInfo
	switch {
	case totalBytes >= networkOverheadCriticalBytes:
		sev = SeverityCritical
	case totalBytes >= networkOverheadWarningBytes:
		sev = SeverityWarning
	}

	msg := fmt.Sprintf("Query returns approximately %s to the client (%d rows × %d bytes). Consider LIMIT, column projection, or server-side aggregation.",
		humanBytes(totalBytes), rows, width)

	// Emit as plan-level (NodeID=0) because the finding is about the
	// query's overall payload, not a specific inner node.
	return []Finding{
		newFinding(r, 0, sev, msg, map[string]any{
			"rows":            rows,
			"row_width_bytes": width,
			"total_bytes":     totalBytes,
			"human_size":      humanBytes(totalBytes),
		}),
	}
}
