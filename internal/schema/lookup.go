package schema

import "strings"

// HasIndexOn reports whether tableFQN has a btree index whose column
// list starts with cols. A longer index such as (a, b, c) covers
// cols [a, b] because the leading columns are the same. Partial
// indexes and non-btree methods are skipped because they don't
// deliver the same coverage as a plain btree.
//
// Examples:
//
//	idx cols: [a, b, c],   request: [a]       → true  (prefix match)
//	idx cols: [a, b, c],   request: [a, b]    → true
//	idx cols: [a, b, c],   request: [a, b, c] → true
//	idx cols: [a, b, c],   request: [a, b, d] → false (fourth column mismatches)
//	idx cols: [b, a],      request: [a]       → false (ordering matters)
//	idx cols: [a] partial, request: [a]       → false (partial indexes don't cover all rows)
//	idx cols: [a] GIN,     request: [a]       → false (non-btree is special-purpose)
func (s *Schema) HasIndexOn(tableFQN string, cols []string) bool {
	if s == nil || len(cols) == 0 {
		return false
	}
	normTable := normaliseFQN(tableFQN)
	for _, idx := range s.Indexes {
		if !indexMatches(idx, normTable) {
			continue
		}
		if len(idx.Columns) < len(cols) {
			continue
		}
		if columnsEqual(idx.Columns[:len(cols)], cols) {
			return true
		}
	}
	return false
}

// FindIndexPrefixing returns an existing index on tableFQN whose
// column list is a strict prefix of cols — in other words, an index
// that partially covers the request and could be extended to cover
// the rest.
//
// Examples with request [a, b, c]:
//
//	idx cols: [a]       → candidate (shorter, leading columns match)
//	idx cols: [a, b]    → better candidate (longer prefix)
//	idx cols: [a, b, c] → NOT returned (already an exact match; use HasIndexOn)
//	idx cols: [b, a]    → NOT returned (ordering differs)
//	idx cols: [a, b, d] → NOT returned (diverges at position 2)
//
// When multiple candidates exist, the longest prefix wins. Returns
// nil when no prefix index exists or when cols is already fully
// covered by some index (callers should check HasIndexOn for that
// case).
func (s *Schema) FindIndexPrefixing(tableFQN string, cols []string) *Index {
	if s == nil || len(cols) < 2 {
		return nil
	}
	normTable := normaliseFQN(tableFQN)
	if s.HasIndexOn(tableFQN, cols) {
		return nil
	}
	var best *Index
	for _, idx := range s.Indexes {
		if !indexMatches(idx, normTable) {
			continue
		}
		if len(idx.Columns) == 0 || len(idx.Columns) >= len(cols) {
			continue
		}
		if !columnsEqual(idx.Columns, cols[:len(idx.Columns)]) {
			continue
		}
		if best == nil || len(idx.Columns) > len(best.Columns) {
			best = idx
		}
	}
	return best
}

// IndexesOn returns every index declared on tableFQN. The returned
// slice is freshly allocated; callers may sort it.
func (s *Schema) IndexesOn(tableFQN string) []*Index {
	if s == nil {
		return nil
	}
	normTable := normaliseFQN(tableFQN)
	var out []*Index
	for _, idx := range s.Indexes {
		if normaliseFQN(idx.Table) == normTable {
			out = append(out, idx)
		}
	}
	return out
}

// FindTable returns a table by fully qualified or bare name. Bare
// names prefer "public" and fall back to any single match across
// schemas; an ambiguous name (multiple matches) returns nil.
func (s *Schema) FindTable(name string) *Table {
	if s == nil || name == "" {
		return nil
	}
	if strings.Contains(name, ".") {
		return s.Tables[normaliseFQN(name)]
	}
	if t, ok := s.Tables[qualify("public", name)]; ok {
		return t
	}
	var match *Table
	for _, t := range s.Tables {
		if t.Name == name {
			if match != nil {
				return nil // ambiguous
			}
			match = t
		}
	}
	return match
}

// ForeignKeysOn returns every foreign key declared on tableFQN.
func (s *Schema) ForeignKeysOn(tableFQN string) []ForeignKey {
	if s == nil {
		return nil
	}
	normTable := normaliseFQN(tableFQN)
	var out []ForeignKey
	for _, fk := range s.FKeys {
		if qualify(fk.Schema, fk.Table) == normTable {
			out = append(out, fk)
		}
	}
	return out
}

// FKWithoutIndex pairs a foreign key with the columns that lack a
// supporting index. Missing is always non-empty.
type FKWithoutIndex struct {
	FK      ForeignKey
	Missing []string
}

// FKColumnsWithoutIndex returns every foreign key whose local columns
// are not covered by a leading-column btree index. The list is
// ordered by (table FQN, fk name) for deterministic output.
//
// Coverage check: we look for an index whose leading columns equal
// the FK's column list (ordering sensitive). An index on (a, b, c)
// covers an FK on (a, b) because a query "WHERE a = ? AND b = ?"
// can use the leading-prefix range. An FK on (b, a) is NOT covered
// by an index on (a, b).
func (s *Schema) FKColumnsWithoutIndex() []FKWithoutIndex {
	if s == nil {
		return nil
	}
	var out []FKWithoutIndex
	for _, fk := range s.FKeys {
		tableFQN := qualify(fk.Schema, fk.Table)
		if s.HasIndexOn(tableFQN, fk.Columns) {
			continue
		}
		out = append(out, FKWithoutIndex{FK: fk, Missing: fk.Columns})
	}
	return out
}

// indexMatches reports whether idx targets normTable and would
// satisfy a btree lookup (non-partial, btree method).
func indexMatches(idx *Index, normTable string) bool {
	if idx == nil {
		return false
	}
	if normaliseFQN(idx.Table) != normTable {
		return false
	}
	if idx.IsPartial {
		return false
	}
	if idx.Method != "" && idx.Method != "btree" {
		return false
	}
	return true
}

// columnsEqual reports whether two column slices are identical by
// position.
func columnsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// normaliseFQN canonicalises schema-qualified names. Bare names are
// assumed to live in "public".
func normaliseFQN(name string) string {
	if name == "" {
		return ""
	}
	if strings.Contains(name, ".") {
		return name
	}
	return qualify("public", name)
}
