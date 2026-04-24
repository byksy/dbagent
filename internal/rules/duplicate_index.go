package rules

import (
	"fmt"
	"strings"

	"github.com/byksy/dbagent/internal/schema"
)

// DuplicateIndex flags pairs of indexes that cover identical column
// lists on the same table. Only fires on tables the current plan
// touches, to keep output scoped to the query under analysis.
type DuplicateIndex struct{}

func (*DuplicateIndex) ID() string         { return "duplicate_index" }
func (*DuplicateIndex) Name() string       { return "Duplicate index" }
func (*DuplicateIndex) Category() Category { return CategoryPrescriptive }

func (r *DuplicateIndex) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Schema == nil {
		return nil
	}
	groups := ctx.Schema.DuplicateIndexes()
	if len(groups) == 0 {
		return nil
	}
	touched := collectTouchedTables(ctx.Plan)
	if len(touched) == 0 {
		return nil
	}

	var out []Finding
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		tableFQN := group[0].Table
		nodeID, ok := touched[tableFQN]
		if !ok {
			continue
		}

		// Pick the drop candidate: prefer a non-primary, non-unique
		// index. If both are constraint-backed, decline to suggest a
		// DROP — the finding is still emitted as a hint.
		dropCandidate := pickDropCandidate(group)

		msg := fmt.Sprintf("Table %s has duplicate indexes on (%s): %s. Consider dropping one.",
			tableFQN,
			strings.Join(group[0].Columns, ", "),
			joinIndexNames(group),
		)

		ev := map[string]any{
			"table":       tableFQN,
			"columns":     group[0].Columns,
			"index_names": indexNamesFromGroup(group),
		}

		f := newFinding(r, nodeID, SeverityWarning, msg, ev)
		if dropCandidate != nil {
			f.Suggested = fmt.Sprintf("DROP INDEX %s;", dropCandidate.FQN())
			f.SuggestedMeta = map[string]any{
				"kind":  "drop_index",
				"index": dropCandidate.Name,
				"table": tableFQN,
			}
		}
		out = append(out, f)
	}
	return out
}

// pickDropCandidate returns the index most safe to DROP from a
// duplicate group. Strategy:
//   - Never pick a primary-key or unique index (they back constraints).
//   - Among the remaining, pick the one with the lexicographically
//     later name — a simple proxy for "newer / less foundational".
//   - If every index in the group is constraint-backed, return nil.
//     The caller keeps the finding but omits the Suggested line.
func pickDropCandidate(group []*schema.Index) *schema.Index {
	var safe []*schema.Index
	for _, idx := range group {
		if idx == nil || idx.IsPrimary || idx.IsUnique {
			continue
		}
		safe = append(safe, idx)
	}
	if len(safe) == 0 {
		return nil
	}
	best := safe[0]
	for _, idx := range safe[1:] {
		if idx.Name > best.Name {
			best = idx
		}
	}
	return best
}

// joinIndexNames returns a human-readable "a and b" / "a, b, c" list
// of index names from a duplicate group.
func joinIndexNames(group []*schema.Index) string {
	names := indexNamesFromGroup(group)
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
}

// indexNamesFromGroup extracts names from a duplicate-index group.
// Kept separate from the generic indexNames helper so callers passing
// a []*schema.Index (not []*schemaIndex-aliased) stay readable.
func indexNamesFromGroup(group []*schema.Index) []string {
	out := make([]string, 0, len(group))
	for _, idx := range group {
		out = append(out, idx.Name)
	}
	return out
}
