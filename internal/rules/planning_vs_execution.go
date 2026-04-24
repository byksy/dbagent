package rules

import (
	"fmt"

	"github.com/byksy/dbagent/internal/plan"
)

// planningVsExecutionMaxExecMs: the rule only makes sense for very
// fast queries — for anything slower, planning time is a rounding
// error, not a target for PREPARE.
const planningVsExecutionMaxExecMs = 10.0

// PlanningVsExecution flags queries where planning takes longer than
// execution. For frequently-run short queries, PREPARE/EXECUTE can
// amortise the planning cost across calls.
type PlanningVsExecution struct{}

func (*PlanningVsExecution) ID() string         { return "planning_vs_execution" }
func (*PlanningVsExecution) Name() string       { return "Planning exceeds execution" }
func (*PlanningVsExecution) Category() Category { return CategoryPrescriptive }

func (r *PlanningVsExecution) Check(p *plan.Plan) []Finding {
	if p == nil {
		return nil
	}
	if p.PlanningTimeMs <= 0 || p.ExecutionTimeMs <= 0 {
		return nil
	}
	if p.PlanningTimeMs <= p.ExecutionTimeMs {
		return nil
	}
	if p.ExecutionTimeMs >= planningVsExecutionMaxExecMs {
		return nil
	}

	ratio := p.PlanningTimeMs / p.ExecutionTimeMs
	msg := fmt.Sprintf("Planning (%.1fms) exceeds execution (%.1fms). Consider PREPARE/EXECUTE for frequent, fast queries.",
		p.PlanningTimeMs, p.ExecutionTimeMs)

	return []Finding{
		newFinding(r, 0, SeverityInfo, msg, map[string]any{
			"planning_ms":    p.PlanningTimeMs,
			"execution_ms":   p.ExecutionTimeMs,
			"planning_ratio": ratio,
		}),
	}
}
