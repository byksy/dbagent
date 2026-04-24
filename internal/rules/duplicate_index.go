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

		// Split the group into a keeper plus drop candidates. The
		// keeper is preferably a primary-key or unique index (they
		// back constraints); otherwise it's the lexicographically
		// smallest name, with everything else suggested for DROP.
		// This scales to 3+ duplicate groups — the previous single-
		// pick approach would have left N-1 duplicates behind instead
		// of N-1 drop suggestions.
		dropCandidates := pickDropCandidates(group)

		msg := fmt.Sprintf("Table %s has duplicate indexes on (%s): %s. Consider dropping %s.",
			tableFQN,
			strings.Join(group[0].Columns, ", "),
			joinIndexNames(group),
			dropVerb(len(dropCandidates)),
		)

		ev := map[string]any{
			"table":           tableFQN,
			"columns":         group[0].Columns,
			"index_names":     indexNamesFromGroup(group),
			"drop_candidates": indexNamesFromGroup(dropCandidates),
		}

		f := newFinding(r, nodeID, SeverityWarning, msg, ev)
		if len(dropCandidates) > 0 {
			var b strings.Builder
			for _, idx := range dropCandidates {
				fmt.Fprintf(&b, "DROP INDEX %s;\n", idx.FQN())
			}
			f.Suggested = strings.TrimRight(b.String(), "\n")
			f.SuggestedMeta = map[string]any{
				"kind":    "drop_index",
				"indexes": indexNamesFromGroup(dropCandidates),
				"table":   tableFQN,
			}
		}
		out = append(out, f)
	}
	return out
}

// pickDropCandidates splits a duplicate group into "keep one" plus
// "drop the rest" and returns the drop list.
//
// Keep-selection priority:
//  1. A primary-key index (drops constraint if removed — sacred).
//  2. A unique index (same reason).
//  3. The lexicographically smallest remaining name — simple, stable
//     tie-break that an operator can explain later.
//
// Returns nil when every index in the group is constraint-backed: we
// refuse to suggest dropping any of them, and the caller omits the
// Suggested line.
func pickDropCandidates(group []*schema.Index) []*schema.Index {
	if len(group) < 2 {
		return nil
	}
	// Find a keep candidate, preferring primary, then unique, then
	// lexicographically smallest name among the non-constraint
	// indexes.
	var keep *schema.Index
	for _, idx := range group {
		if idx != nil && idx.IsPrimary {
			keep = idx
			break
		}
	}
	if keep == nil {
		for _, idx := range group {
			if idx != nil && idx.IsUnique {
				keep = idx
				break
			}
		}
	}
	if keep == nil {
		for _, idx := range group {
			if idx == nil {
				continue
			}
			if keep == nil || idx.Name < keep.Name {
				keep = idx
			}
		}
	}

	var drops []*schema.Index
	for _, idx := range group {
		if idx == nil || idx == keep {
			continue
		}
		if idx.IsPrimary || idx.IsUnique {
			// Never propose dropping a constraint-backed index, even
			// if we kept a non-constraint one instead. This shouldn't
			// happen with correct PG schemas, but defends against
			// oddly-shaped test fixtures.
			continue
		}
		drops = append(drops, idx)
	}
	return drops
}

// dropVerb returns singular/plural "one" / "all but one" phrasing
// for the message.
func dropVerb(n int) string {
	if n == 1 {
		return "one"
	}
	return "all but one"
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
