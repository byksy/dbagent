package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/byksy/dbagent/internal/plan"
)

// BitmapAndComposite flags a BitmapAnd built from multiple bitmap
// index scans on the same relation. Combining bitmaps at query time
// is strictly slower than reading a composite index that already
// covers the same predicate, because the BitmapAnd step has to
// allocate and intersect bitmaps.
type BitmapAndComposite struct{}

// bitmapAndRawType is the string PostgreSQL emits; we don't have a
// dedicated NodeType because Stage 2's enum didn't need it. Matching
// on RawNodeType keeps the rule working without touching the plan
// model.
const bitmapAndRawType = "BitmapAnd"

func (*BitmapAndComposite) ID() string         { return "bitmap_and_composite" }
func (*BitmapAndComposite) Name() string       { return "BitmapAnd → composite index" }
func (*BitmapAndComposite) Category() Category { return CategoryPrescriptive }

func (r *BitmapAndComposite) Check(p *plan.Plan) []Finding {
	if p == nil || p.Root == nil {
		return nil
	}
	var out []Finding
	for _, n := range p.AllNodes() {
		if n.NeverExecuted || n.RawNodeType != bitmapAndRawType {
			continue
		}
		children := bitmapIndexChildren(n)
		if len(children) < 2 {
			continue
		}
		relation := FirstRelationName(n)
		if relation == "" {
			continue
		}

		type indexed struct {
			name string
			col  string
			rows int64
		}
		var specs []indexed
		for _, c := range children {
			col := firstIndexCondColumn(c.IndexCond)
			if col == "" {
				continue
			}
			specs = append(specs, indexed{name: c.IndexName, col: col, rows: c.ActualRows})
		}
		if len(specs) < 2 {
			continue
		}

		// Order by selectivity: smallest match count first. Falls back
		// to stable source order for ties.
		sort.SliceStable(specs, func(i, j int) bool {
			return specs[i].rows < specs[j].rows
		})

		cols := make([]string, 0, len(specs))
		names := make([]string, 0, len(specs))
		for _, s := range specs {
			cols = append(cols, s.col)
			if s.name != "" {
				names = append(names, s.name)
			}
		}

		msg := fmt.Sprintf("Planner combined %d separate bitmap index scans. A composite index on (%s) would avoid the BitmapAnd step.",
			len(specs), strings.Join(cols, ", "))

		ev := map[string]any{
			"relation":         relation,
			"index_names":      names,
			"proposed_columns": cols,
		}

		f := newFinding(r, n.ID, SeverityWarning, msg, ev)
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
		out = append(out, f)
	}
	return out
}

// bitmapIndexChildren returns the BitmapIndexScan descendants that
// feed directly into a BitmapAnd. Intermediate BitmapOr nodes are
// not expanded — if the planner used OR, a composite index is the
// wrong suggestion.
func bitmapIndexChildren(n *plan.Node) []*plan.Node {
	var out []*plan.Node
	for _, c := range n.Children {
		if c.NodeType == plan.NodeTypeBitmapIndexScan {
			out = append(out, c)
		}
	}
	return out
}

// firstIndexCondColumn pulls the left-hand column from the first
// predicate of an Index Cond string. Uses the same conservative
// parser as ExtractFilterColumns.
func firstIndexCondColumn(cond string) string {
	cols := ExtractFilterColumns(cond)
	if len(cols) == 0 {
		return ""
	}
	return cols[0]
}
