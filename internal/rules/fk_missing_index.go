package rules

import (
	"fmt"
	"strings"

	"github.com/byksy/dbagent/internal/plan"
	"github.com/byksy/dbagent/internal/schema"
)

// FKMissingIndex flags foreign key columns that lack a supporting
// leading-column btree index. Unlike most rules this one reads its
// signal from the schema, not the plan — but we only surface
// findings for FKs on tables the current plan actually touches, so
// the output stays scoped to "what you're looking at right now".
type FKMissingIndex struct{}

func (*FKMissingIndex) ID() string         { return "fk_missing_index" }
func (*FKMissingIndex) Name() string       { return "Foreign key without index" }
func (*FKMissingIndex) Category() Category { return CategoryPrescriptive }

func (r *FKMissingIndex) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Schema == nil {
		return nil
	}
	missing := ctx.Schema.FKColumnsWithoutIndex()
	if len(missing) == 0 {
		return nil
	}

	touched := collectTouchedTables(ctx.Plan)
	if len(touched) == 0 {
		// No base-table scan in the plan — nothing to anchor findings
		// to. Emitting plan-level findings would spam unrelated FKs,
		// so we stay silent.
		return nil
	}

	var out []Finding
	for _, m := range missing {
		tableFQN := fullyQualified(m.FK.Schema, m.FK.Table)
		nodeID, ok := touched[tableFQN]
		if !ok {
			continue
		}
		out = append(out, buildFKFinding(r, m, nodeID, tableFQN))
	}
	return out
}

// collectTouchedTables walks the plan and returns the first scan
// node's ID for each base relation it encounters. Keys are fully
// qualified names ("schema.relation"); values are node IDs suitable
// for attaching findings.
func collectTouchedTables(p *plan.Plan) map[string]int {
	out := map[string]int{}
	if p == nil || p.Root == nil {
		return out
	}
	p.Root.Walk(func(n *plan.Node) {
		if n.RelationName == "" {
			return
		}
		if !isScanNode(n.NodeType) {
			return
		}
		fqn := fullyQualified(n.Schema, n.RelationName)
		if _, seen := out[fqn]; !seen {
			out[fqn] = n.ID
		}
	})
	return out
}

// fullyQualified prepends "public" when no schema is set, matching
// the convention used by internal/schema. Keeps matching consistent
// between plan node relation names (usually bare) and Schema keys
// (always qualified).
func fullyQualified(s, name string) string {
	if name == "" {
		return ""
	}
	if s == "" {
		return "public." + name
	}
	return s + "." + name
}

// isScanNode reports whether a NodeType reads rows from a user
// table directly. Non-scan nodes (joins, aggregates) don't have a
// RelationName and shouldn't anchor an FK-coverage finding.
func isScanNode(t plan.NodeType) bool {
	switch t {
	case plan.NodeTypeSeqScan,
		plan.NodeTypeIndexScan,
		plan.NodeTypeIndexOnlyScan,
		plan.NodeTypeBitmapHeapScan,
		plan.NodeTypeTidScan,
		plan.NodeTypeForeignScan:
		return true
	}
	return false
}

// buildFKFinding assembles a Finding for a single uncovered FK.
func buildFKFinding(r *FKMissingIndex, m schema.FKWithoutIndex, nodeID int, tableFQN string) Finding {
	msg := fmt.Sprintf("Table %s has a foreign key %q on column(s) (%s) without a supporting index. Joins and cascading operations on this column will be slow.",
		tableFQN, m.FK.Name, strings.Join(m.FK.Columns, ", "))
	refFQN := fullyQualified(m.FK.ReferencedSchema, m.FK.ReferencedTable)
	ev := map[string]any{
		"table":            tableFQN,
		"fk_name":          m.FK.Name,
		"columns":          m.FK.Columns,
		"referenced_table": refFQN,
	}
	f := newFinding(r, nodeID, SeverityWarning, msg, ev)
	f.Suggested = fmt.Sprintf("CREATE INDEX ON %s (%s);", tableFQN, strings.Join(m.FK.Columns, ", "))
	f.SuggestedMeta = map[string]any{
		"kind":     "create_index",
		"relation": m.FK.Table,
		"schema":   m.FK.Schema,
		"columns":  m.FK.Columns,
		"where":    "",
		"method":   "btree",
	}
	return f
}
