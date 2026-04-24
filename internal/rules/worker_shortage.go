package rules

import (
	"fmt"

	"github.com/byksy/dbagent/internal/plan"
)

// WorkerShortage flags Gather nodes that received fewer parallel
// workers than the planner wanted. Either max_parallel_workers is
// too low or concurrent load exhausted the pool.
type WorkerShortage struct{}

func (*WorkerShortage) ID() string         { return "worker_shortage" }
func (*WorkerShortage) Name() string       { return "Parallel worker shortage" }
func (*WorkerShortage) Category() Category { return CategoryDiagnostic }

func (r *WorkerShortage) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Plan == nil || ctx.Plan.Root == nil {
		return nil
	}
	p := ctx.Plan
	var out []Finding
	for _, n := range p.AllNodes() {
		if n.NeverExecuted {
			continue
		}
		if n.NodeType != plan.NodeTypeGather && n.NodeType != plan.NodeTypeGatherMerge {
			continue
		}
		if n.WorkersPlanned == 0 || n.WorkersLaunched >= n.WorkersPlanned {
			continue
		}
		shortfall := n.WorkersPlanned - n.WorkersLaunched
		sev := SeverityInfo
		if shortfall >= 2 {
			sev = SeverityWarning
		}
		msg := fmt.Sprintf("Only %d of %d planned workers were launched. Consider raising max_parallel_workers.",
			n.WorkersLaunched, n.WorkersPlanned)
		out = append(out, newFinding(r, n.ID, sev, msg, map[string]any{
			"workers_planned":  n.WorkersPlanned,
			"workers_launched": n.WorkersLaunched,
			"shortfall":        shortfall,
		}))
	}
	return out
}
