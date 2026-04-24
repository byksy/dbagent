package rules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/byksy/dbagent/internal/plan"
)

// Thresholds for memoize_opportunity. The rule needs enough inner
// iterations for caching to matter, and the inner side must look
// like it's hitting repeated keys (low rows-per-loop).
const (
	memoizeMinInnerLoops       int64   = 100
	memoizeMaxAvgRowsPerLoop   float64 = 1.0
	memoizeMinServerVersion            = 14
)

// MemoizeOpportunity flags Nested Loop nodes where the inner side
// looks like a prime Memoize candidate (PG 14+) but the planner
// didn't choose one. Usually the culprit is stale statistics; the
// rule nudges the operator toward ANALYZE.
type MemoizeOpportunity struct{}

func (*MemoizeOpportunity) ID() string         { return "memoize_opportunity" }
func (*MemoizeOpportunity) Name() string       { return "Memoize would amortise repeated lookups" }
func (*MemoizeOpportunity) Category() Category { return CategoryPrescriptive }

func (r *MemoizeOpportunity) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Plan == nil || ctx.Plan.Root == nil {
		return nil
	}
	if v := planServerMajorVersion(ctx.Plan); v != 0 && v < memoizeMinServerVersion {
		return nil
	}
	var out []Finding
	for _, n := range ctx.Plan.AllNodes() {
		if n.NeverExecuted || n.NodeType != plan.NodeTypeNestedLoop {
			continue
		}
		inner := nestedLoopInner(n)
		if inner == nil || inner.NeverExecuted {
			continue
		}
		if inner.Loops < memoizeMinInnerLoops {
			continue
		}
		if float64(inner.ActualRows) > memoizeMaxAvgRowsPerLoop {
			continue
		}
		if hasMemoizeBetween(n, inner) {
			continue
		}

		msg := fmt.Sprintf("Nested Loop with %d inner iterations averaging %.1f rows each. A Memoize node could cache repeated lookups; check ANALYZE freshness and enable_memoize.",
			inner.Loops, float64(inner.ActualRows))

		ev := map[string]any{
			"loops":             inner.Loops,
			"avg_rows_per_loop": float64(inner.ActualRows),
			"inner_node_type":   inner.NodeType.String(),
			"pg_version":        ctx.Plan.Settings["server_version"],
		}
		f := newFinding(r, n.ID, SeverityInfo, msg, ev)
		if inner.RelationName != "" {
			f.Suggested = fmt.Sprintf("ANALYZE %s;", qualifiedRelation(inner, inner.RelationName))
			f.SuggestedMeta = map[string]any{
				"kind":     "analyze",
				"relation": inner.RelationName,
				"schema":   inner.Schema,
			}
		}
		out = append(out, f)
	}
	return out
}

// nestedLoopInner returns the inner-side child of a Nested Loop —
// the one that reruns per outer row. PostgreSQL tags it with
// `Parent Relationship: "Inner"`.
func nestedLoopInner(n *plan.Node) *plan.Node {
	for _, c := range n.Children {
		if c == nil {
			continue
		}
		if strings.EqualFold(c.ParentRel, "Inner") {
			return c
		}
	}
	return nil
}

// hasMemoizeBetween checks whether a Memoize node sits between the
// Nested Loop and its inner scan (PG inserts one directly above the
// cached subtree when it chooses to memoize).
func hasMemoizeBetween(outer, inner *plan.Node) bool {
	// The inner pointer we received is the direct child; if it's
	// already a Memoize, the rule shouldn't fire.
	if inner.NodeType == plan.NodeTypeMemoize {
		return true
	}
	// Defensive walk in case future parser changes introduce a
	// wrapper: ascend from inner until we hit outer.
	for cur := inner; cur != nil && cur != outer; cur = cur.Parent {
		if cur.NodeType == plan.NodeTypeMemoize {
			return true
		}
	}
	return false
}

// planServerMajorVersion extracts the major version number of the
// PostgreSQL server that produced the plan, reading from
// Plan.Settings when present. Returns 0 when unknown — callers treat
// that as "skip the version check", preferring to over-fire rather
// than silently swallow findings.
func planServerMajorVersion(p *plan.Plan) int {
	if p == nil {
		return 0
	}
	raw := strings.TrimSpace(p.Settings["server_version"])
	if raw == "" {
		return 0
	}
	// Strip everything after the first non-digit-or-dot character so
	// "17.2 (Debian 17.2-1.pgdg13+1)" parses cleanly.
	end := len(raw)
	for i, r := range raw {
		if r != '.' && (r < '0' || r > '9') {
			end = i
			break
		}
	}
	raw = raw[:end]
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) == 0 {
		return 0
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major <= 0 {
		return 0
	}
	return major
}
