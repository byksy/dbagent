package plan

import "strings"

// ExclusiveTimeMs returns the wall-clock time spent in this node only,
// excluding children and multiplied by Loops. InitPlan- and
// CTE-source children are NOT subtracted — their time is already
// accounted for where the plan uses them, at the CTE Scan call sites.
// Returns 0 for NeverExecuted nodes and clamps floating-point
// roundoff to 0.
func (n *Node) ExclusiveTimeMs() float64 {
	if n == nil || n.NeverExecuted {
		return 0
	}
	inclusive := n.ActualTotalTimeMs * float64(loopsOrOne(n))
	var childSum float64
	for _, c := range n.Children {
		if isInitPlanOrCTE(c) {
			continue
		}
		childSum += c.InclusiveTimeMs()
	}
	if v := inclusive - childSum; v > 0 {
		return v
	}
	return 0
}

// InclusiveTimeMs returns the total time spent in this node and all
// children, multiplied by Loops. Returns 0 for NeverExecuted nodes.
func (n *Node) InclusiveTimeMs() float64 {
	if n == nil || n.NeverExecuted {
		return 0
	}
	return n.ActualTotalTimeMs * float64(loopsOrOne(n))
}

// ActualRowsTotal returns the total row count across loops and
// parallel workers. PostgreSQL emits ActualRows as a per-loop,
// per-worker figure for parallel leaves, so we multiply by both Loops
// and the parallel-row multiplier.
func (n *Node) ActualRowsTotal() int64 {
	if n == nil || n.NeverExecuted {
		return 0
	}
	return n.ActualRows * loopsOrOne(n) * parallelRowMultiplier(n)
}

// MisestimateFactor returns max(actual/planned, planned/actual). A
// PlanRows or ActualRowsTotal of 0 returns 0 so callers can short-
// circuit "no data" rather than dividing by zero.
func (n *Node) MisestimateFactor() float64 {
	if n == nil || n.NeverExecuted {
		return 0
	}
	planned := float64(n.PlanRows)
	actual := float64(n.ActualRowsTotal())
	if planned <= 0 || actual <= 0 {
		return 0
	}
	if actual > planned {
		return actual / planned
	}
	return planned / actual
}

// MisestimateDirection returns +1 when the planner underestimated
// (actual > planned), -1 when it overestimated, 0 when equal or data
// is missing.
func (n *Node) MisestimateDirection() int {
	if n == nil || n.NeverExecuted {
		return 0
	}
	planned := n.PlanRows
	actual := n.ActualRowsTotal()
	if planned == 0 || actual == 0 {
		return 0
	}
	switch {
	case actual > planned:
		return +1
	case actual < planned:
		return -1
	}
	return 0
}

// CacheHitRatio returns SharedHit / (SharedHit + SharedRead) as a
// fraction in [0,1]. Returns -1 when no buffer data is available, so
// callers can distinguish "zero hits" from "no info".
func (n *Node) CacheHitRatio() float64 {
	if n == nil {
		return -1
	}
	total := n.SharedHitBlocks + n.SharedReadBlocks
	if total == 0 {
		return -1
	}
	return float64(n.SharedHitBlocks) / float64(total)
}

// FilterRemovalRatio returns RowsRemovedByFilter divided by the number
// of rows examined (kept + removed). Returns -1 when neither figure is
// available.
func (n *Node) FilterRemovalRatio() float64 {
	if n == nil {
		return -1
	}
	total := n.ActualRows*loopsOrOne(n) + n.RowsRemovedByFilter
	if total == 0 {
		return -1
	}
	return float64(n.RowsRemovedByFilter) / float64(total)
}

// Walk calls fn on n and every descendant in depth-first pre-order.
// fn is called once per node; nil-safe on n.
func (n *Node) Walk(fn func(*Node)) {
	if n == nil || fn == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		c.Walk(fn)
	}
}

// Find returns the first node for which pred returns true, or nil.
func (p *Plan) Find(pred func(*Node) bool) *Node {
	if p == nil || p.Root == nil || pred == nil {
		return nil
	}
	var found *Node
	p.Root.Walk(func(n *Node) {
		if found == nil && pred(n) {
			found = n
		}
	})
	return found
}

// AllNodes returns every node in the plan in depth-first pre-order.
func (p *Plan) AllNodes() []*Node {
	if p == nil || p.Root == nil {
		return nil
	}
	var out []*Node
	p.Root.Walk(func(n *Node) { out = append(out, n) })
	return out
}

// parallelRowMultiplier returns the multiplier to apply to a parallel
// leaf's per-worker ActualRows to get the total. PostgreSQL reports
// parallel scan actuals as a per-worker average, and the leader
// participates too, so total rows = ActualRows × (WorkersLaunched + 1)
// for parallel-aware scan nodes and 1 otherwise.
func parallelRowMultiplier(n *Node) int64 {
	if n == nil || n.WorkersLaunched == 0 {
		return 1
	}
	m := int64(n.WorkersLaunched + 1)
	if m < 1 {
		return 1
	}
	return m
}

// loopsOrOne returns n.Loops when positive, else 1. Needed because
// some synthetic nodes (and never-executed-but-reached branches)
// emit Loops=0 via the NeverExecuted path, which the callers above
// already short-circuit — so this is only for the normal path.
func loopsOrOne(n *Node) int64 {
	if n.Loops > 0 {
		return n.Loops
	}
	return 1
}

// isInitPlanOrCTE reports whether n's time should be excluded from
// its parent's exclusive-time subtraction. PostgreSQL emits InitPlans
// and CTE source definitions as children of the node that introduces
// the subplan, but their runtime actually shows up at the CTE Scan /
// SubPlan reference sites — subtracting them here would double-count.
//
// Observed shapes (PG 13-17):
//   - InitPlan: Parent Relationship = "InitPlan", Subplan Name begins
//     with "InitPlan ".
//   - CTE source: Parent Relationship = "InitPlan" (yes, "InitPlan" —
//     PostgreSQL reuses the label for non-recursive CTE definitions),
//     Subplan Name begins with "CTE ".
func isInitPlanOrCTE(n *Node) bool {
	if n == nil {
		return false
	}
	if n.ParentRel == "InitPlan" {
		return true
	}
	if n.ParentRel == "Subquery" && (strings.HasPrefix(n.SubplanName, "CTE ") || strings.HasPrefix(n.SubplanName, "InitPlan ")) {
		return true
	}
	return false
}
