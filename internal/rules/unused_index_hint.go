package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/byksy/dbagent/internal/schema"
)

// UnusedIndexHint surfaces non-primary, non-unique indexes with zero
// recorded scans, but intentionally does not suggest DROP. pg_stat
// counters reset when stats are cleared and can be misleading on
// short-lived databases; the rule exists to alert, not to automate.
//
// The rule requires a schema export from a version of dbagent new
// enough to carry the Scans field. Older exports load with Scans=0
// everywhere, which would make every index look unused — that's why
// we additionally require the Scans field to be plausibly populated
// (at least one index in the touched set has Scans > 0).
type UnusedIndexHint struct{}

func (*UnusedIndexHint) ID() string         { return "unused_index_hint" }
func (*UnusedIndexHint) Name() string       { return "Potentially unused index" }
func (*UnusedIndexHint) Category() Category { return CategoryDiagnostic }

func (r *UnusedIndexHint) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Schema == nil {
		return nil
	}
	touched := collectTouchedTables(ctx.Plan)
	if len(touched) == 0 {
		return nil
	}
	// Guard against old schema exports where Scans is zero simply
	// because the field wasn't populated: if no touched index has
	// scans > 0 we can't distinguish "unused" from "unknown".
	if !anyIndexHasScans(ctx.Schema, touched) {
		return nil
	}

	var out []Finding
	// Deterministic iteration: sort tables by FQN so findings order
	// stays stable across map iterations.
	tables := make([]string, 0, len(touched))
	for t := range touched {
		tables = append(tables, t)
	}
	sort.Strings(tables)

	for _, tableFQN := range tables {
		nodeID := touched[tableFQN]
		unused := unusedIndexesOn(ctx.Schema, tableFQN)
		if len(unused) == 0 {
			continue
		}
		names := indexNames(unused)
		msg := fmt.Sprintf("Table %s has %d index(es) with zero scans recorded since last stats reset: %s. Review whether they're still needed. dbagent does not generate DROP INDEX statements automatically.",
			tableFQN, len(names), strings.Join(names, ", "))
		out = append(out, newFinding(r, nodeID, SeverityInfo, msg, map[string]any{
			"table":           tableFQN,
			"unused_indexes":  names,
			"unused_count":    len(names),
		}))
	}
	return out
}

// anyIndexHasScans returns true if any index on the touched tables
// has Scans > 0 — i.e., the Scans field was populated by a fresh
// schema export. We use this as a heuristic to avoid firing on
// stale/pre-Stage-5 JSON exports where the field is uniformly zero.
func anyIndexHasScans(s *schema.Schema, touched map[string]int) bool {
	for tableFQN := range touched {
		for _, idx := range s.IndexesOn(tableFQN) {
			if idx.Scans > 0 {
				return true
			}
		}
	}
	return false
}

// unusedIndexesOn returns non-primary, non-unique indexes on the
// given table that have zero recorded scans. Primary/unique indexes
// are filtered because their "scan count" is not a reliable proxy
// for usefulness — they enforce constraints regardless.
func unusedIndexesOn(s *schema.Schema, tableFQN string) []*schema.Index {
	var out []*schema.Index
	for _, idx := range s.IndexesOn(tableFQN) {
		if idx.IsPrimary || idx.IsUnique {
			continue
		}
		if idx.Scans > 0 {
			continue
		}
		out = append(out, idx)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// indexNames collapses a slice of indexes to their names.
func indexNames(xs []*schema.Index) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.Name)
	}
	return out
}
