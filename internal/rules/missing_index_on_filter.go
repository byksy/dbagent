package rules

import (
	"fmt"
	"strings"

	"github.com/byksy/dbagent/internal/plan"
	"github.com/byksy/dbagent/internal/schema"
)

// Volume thresholds. The 4× factor means the filter must have
// removed at least four times the rows it kept — equivalently, at
// least 80% of what was scanned was discarded. The earlier 5× value
// drifted the gate to 83.33% and made queries that hit the 80%
// threshold exactly silently escape the rule; 4× puts the arithmetic
// in line with the comment. The loops*removed gate suppresses the
// rule on tiny tables where a seq scan is fine.
const (
	missingIndexRemovalFactor   = 4
	missingIndexMinWeightedRows = 100
	missingIndexCriticalRows    = 10_000
)

// MissingIndexOnFilter is the prescriptive counterpart to
// FilterRemovalRatio: when a filter is discarding the bulk of a
// scan's rows at meaningful volume, propose an index on the
// filter's columns. When a schema is available the rule also checks
// for existing coverage and, where possible, suggests extending a
// shorter index rather than creating a duplicate.
type MissingIndexOnFilter struct{}

func (*MissingIndexOnFilter) ID() string         { return "missing_index_on_filter" }
func (*MissingIndexOnFilter) Name() string       { return "Missing index on filter" }
func (*MissingIndexOnFilter) Category() Category { return CategoryPrescriptive }

func (r *MissingIndexOnFilter) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Plan == nil || ctx.Plan.Root == nil {
		return nil
	}
	p := ctx.Plan
	var out []Finding
	for _, n := range p.AllNodes() {
		if n.NeverExecuted || !missingIndexApplies(n) {
			continue
		}
		// The ratio gate stays on per-loop figures (the filter's
		// selectivity per invocation is what matters for "index would
		// help"), but volume gates and user-facing numbers use
		// loop-scaled totals so parallel / nested-loop inner scans
		// don't under-report.
		removedPerLoop := n.RowsRemovedByFilter
		keptPerLoop := n.ActualRows
		if keptPerLoop*int64(missingIndexRemovalFactor) >= removedPerLoop {
			continue
		}
		loops := n.Loops
		if loops < 1 {
			loops = 1
		}
		removedTotal := removedPerLoop * loops
		keptTotal := keptPerLoop * loops
		if removedTotal < missingIndexMinWeightedRows {
			continue
		}

		relation := FirstRelationName(n)
		cols := ExtractFilterColumns(n.Filter)
		// tableFQN is used for schema lookup only — the user-facing
		// Suggested retains the Stage 3 form (bare relation) unless
		// the operator explicitly set a schema on the plan node.
		tableFQN := relation
		if n.Schema != "" && relation != "" {
			tableFQN = n.Schema + "." + relation
		}

		// Schema-aware suppression: if the filter's column(s) are
		// already covered by a non-partial btree, this rule has no
		// useful advice. The observed filter volume likely indicates
		// poor index selectivity — which belongs to a different (not
		// yet written) rule — rather than a missing index.
		if ctx.Schema != nil && relation != "" && len(cols) > 0 {
			if ctx.Schema.HasIndexOn(tableFQN, cols) {
				continue
			}
		}

		sev := SeverityWarning
		if removedTotal > missingIndexCriticalRows {
			sev = SeverityCritical
		}

		ratio := 1.0
		if keptTotal+removedTotal > 0 {
			ratio = float64(removedTotal) / float64(keptTotal+removedTotal)
		}

		msg := fmt.Sprintf("Filter removes %.0f%% of rows scanned (%d rows). Consider an index to push the predicate down.",
			ratio*100, removedTotal)

		ev := map[string]any{
			"rows_removed":   removedTotal,
			"rows_kept":      keptTotal,
			"loops":          loops,
			"filter":         n.Filter,
			"parsed_columns": cols,
			"relation":       relation,
		}

		f := newFinding(r, n.ID, sev, msg, ev)

		// Prefer extending an existing prefix index over creating a
		// duplicate. The extension variant only applies when schema
		// is loaded and the filter decomposed into ≥ 2 columns.
		extended := false
		if ctx.Schema != nil && relation != "" && len(cols) > 1 {
			if existing := ctx.Schema.FindIndexPrefixing(tableFQN, cols); existing != nil {
				f.Message = fmt.Sprintf("Filter removes %.0f%% of rows scanned (%d rows). Existing index %q could be extended to cover this.",
					ratio*100, removedTotal, existing.Name)
				f.Suggested = buildExtendIndexSQL(existing, qualifiedRelation(n, relation), cols)
				f.SuggestedMeta = map[string]any{
					"kind":           "extend_index",
					"existing_index": existing.Name,
					"relation":       relation,
					"schema":         n.Schema,
					"columns":        cols,
					"where":          "",
					"method":         "btree",
				}
				ev["existing_index"] = existing.Name
				extended = true
			}
		}

		if !extended && relation != "" && len(cols) > 0 {
			f.Suggested = fmt.Sprintf("CREATE INDEX ON %s (%s);",
				qualifiedRelation(n, relation), strings.Join(cols, ", "))
			f.SuggestedMeta = map[string]any{
				"kind":     "create_index",
				"relation": relation,
				"schema":   n.Schema,
				"columns":  cols,
				"where":    "",
				"method":   "btree",
			}
		}

		// Note schema absence so operators reading the finding know
		// the suggestion wasn't verified against the live DB.
		if ctx.Schema == nil && relation != "" && len(cols) > 0 {
			f.Message += " (schema not available to verify)"
		}

		out = append(out, f)
	}
	return out
}

// buildExtendIndexSQL produces a DROP + CREATE pair that turns an
// existing prefix index into a composite that covers cols. We do
// not use CREATE INDEX CONCURRENTLY here — safer to let the operator
// opt in to that when they deploy.
func buildExtendIndexSQL(existing *schema.Index, tableFQN string, cols []string) string {
	return fmt.Sprintf("-- Existing: %s\n-- Replace with:\nDROP INDEX %s;\nCREATE INDEX ON %s (%s);",
		existing.Definition,
		existing.FQN(),
		tableFQN,
		strings.Join(cols, ", "),
	)
}

// missingIndexApplies gates the rule to scans that have a Filter
// clause (not just Index Cond). Index Cond predicates already use
// the index, so suggesting "another index" there would be wrong.
func missingIndexApplies(n *plan.Node) bool {
	if n.Filter == "" {
		return false
	}
	switch n.NodeType {
	case plan.NodeTypeSeqScan,
		plan.NodeTypeBitmapHeapScan,
		plan.NodeTypeIndexScan,
		plan.NodeTypeIndexOnlyScan:
		return true
	}
	return false
}

// qualifiedRelation prepends schema when known. The relation name
// argument is passed separately because FirstRelationName may have
// walked up the tree to find it. Stage 3 produced bare relation
// names (matching PG's Relation Name output); we keep that shape so
// Suggested SQL remains copy-pasteable without assuming public.
func qualifiedRelation(n *plan.Node, relation string) string {
	if n.Schema != "" {
		return n.Schema + "." + relation
	}
	return relation
}
