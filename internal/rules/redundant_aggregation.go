package rules

import (
	"strings"

	"github.com/byksy/dbagent/internal/plan"
)

// RedundantAggregation flags `HashAggregate` placed on top of a
// `Sort` whose keys already group the input. When Sort's keys start
// with the aggregate's GROUP BY columns, the planner could have
// picked GroupAggregate directly and skipped the hash table. This is
// an optimiser hint — the user may not be able to change it without
// touching the query shape or GUCs — so we keep severity low.
type RedundantAggregation struct{}

func (*RedundantAggregation) ID() string         { return "redundant_aggregation" }
func (*RedundantAggregation) Name() string       { return "HashAggregate over Sort" }
func (*RedundantAggregation) Category() Category { return CategoryPrescriptive }

func (r *RedundantAggregation) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Plan == nil || ctx.Plan.Root == nil {
		return nil
	}
	var out []Finding
	for _, n := range ctx.Plan.AllNodes() {
		if n.NeverExecuted {
			continue
		}
		if n.NodeType != plan.NodeTypeAggregate || n.Strategy != "Hashed" {
			continue
		}
		child := firstNonInitPlanChild(n)
		if child == nil || child.NodeType != plan.NodeTypeSort {
			continue
		}
		if len(n.GroupKey) == 0 || len(child.SortKey) == 0 {
			continue
		}
		sortCols := sortKeyColumns(child.SortKey)
		if !prefixCovers(sortCols, n.GroupKey) {
			continue
		}

		msg := "HashAggregate above a Sort that could feed GroupAggregate directly. Consider enforcing GroupAggregate via query shape or enable_hashagg settings."
		out = append(out, newFinding(r, n.ID, SeverityInfo, msg, map[string]any{
			"group_key":            n.GroupKey,
			"sort_key":             child.SortKey,
			"sort_feeds_group_key": true,
		}))
	}
	return out
}

// firstNonInitPlanChild returns the first child whose parent
// relationship isn't "InitPlan", so the rule looks at the actual
// row pipeline under the aggregate. PostgreSQL reuses the
// "InitPlan" parent-relationship label for both classic InitPlans
// and non-recursive CTE source definitions (see parser comments in
// internal/plan/stats.go), so a single skip covers both cases.
func firstNonInitPlanChild(n *plan.Node) *plan.Node {
	for _, c := range n.Children {
		if c == nil {
			continue
		}
		if c.ParentRel == "InitPlan" {
			continue
		}
		return c
	}
	return nil
}

// sortKeyColumns strips ordering modifiers off Sort Key entries
// ("customer_id DESC" → "customer_id") so they can be compared to
// GROUP BY columns, which are always bare.
func sortKeyColumns(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		// Drop trailing NULLS FIRST/LAST first so the ordering token
		// is at the end of the remainder.
		k = trimSuffixFold(k, " NULLS FIRST")
		k = trimSuffixFold(k, " NULLS LAST")
		k = trimSuffixFold(k, " DESC")
		k = trimSuffixFold(k, " ASC")
		out = append(out, strings.TrimSpace(k))
	}
	return out
}

// trimSuffixFold strips a case-insensitive suffix when present.
func trimSuffixFold(s, suffix string) string {
	if len(s) >= len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix) {
		return s[:len(s)-len(suffix)]
	}
	return s
}

// prefixCovers reports whether the first len(need) entries of have
// equal need. Used to check that a Sort's leading keys cover a
// GROUP BY's columns.
func prefixCovers(have, need []string) bool {
	if len(have) < len(need) {
		return false
	}
	for i, n := range need {
		if have[i] != n {
			return false
		}
	}
	return true
}
