package rules

import (
	"fmt"

	"github.com/byksy/dbagent/internal/plan"
)

// sortSpilledCriticalKB: above 1GB on disk, spilling is unquestionably
// a problem regardless of query. Below that, it's annoying but some
// sorts genuinely need a lot of space and that's a capacity call.
const sortSpilledCriticalKB = 1_000_000

// SortSpilled flags Sort nodes that overflowed work_mem and spilled
// to disk. The remediation is always "raise work_mem".
type SortSpilled struct{}

func (*SortSpilled) ID() string         { return "sort_spilled" }
func (*SortSpilled) Name() string       { return "Sort spilled to disk" }
func (*SortSpilled) Category() Category { return CategoryPrescriptive }

func (r *SortSpilled) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Plan == nil || ctx.Plan.Root == nil {
		return nil
	}
	p := ctx.Plan
	var out []Finding
	for _, n := range p.AllNodes() {
		if n.NeverExecuted {
			continue
		}
		if n.NodeType != plan.NodeTypeSort && n.NodeType != plan.NodeTypeIncrementalSort {
			continue
		}
		if n.SortSpaceType != "Disk" {
			continue
		}

		sev := SeverityWarning
		if n.SortSpaceKB > sortSpilledCriticalKB {
			sev = SeverityCritical
		}

		recommendedKB := int64(float64(n.SortSpaceKB) * 1.2)
		workMem := FormatWorkMem(recommendedKB)

		msg := fmt.Sprintf("Sort spilled to disk (%dkB). Increasing work_mem would keep it in memory.",
			n.SortSpaceKB)

		ev := map[string]any{
			"sort_space_kb": n.SortSpaceKB,
			"sort_method":   n.SortMethod,
			"sort_key":      n.SortKey,
		}

		f := newFinding(r, n.ID, sev, msg, ev)
		f.Suggested = fmt.Sprintf("SET LOCAL work_mem = '%s';", workMem)
		f.SuggestedMeta = map[string]any{
			"kind":                "set_work_mem",
			"current_kb_estimate": 4096, // PostgreSQL default 4MB when unknown
			"recommended_kb":      recommendedKB,
		}
		out = append(out, f)
	}
	return out
}
