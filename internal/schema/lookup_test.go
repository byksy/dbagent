package schema

import "testing"

// buildSchema is a concise helper that returns a Schema with a single
// public.orders table and an ordered set of indexes. Used by the
// table-driven tests below to keep setup boilerplate out of each case.
func buildSchema(indexes ...*Index) *Schema {
	s := &Schema{
		Tables: map[string]*Table{
			"public.orders": {
				Schema: "public",
				Name:   "orders",
			},
		},
		Indexes: map[string]*Index{},
	}
	for _, idx := range indexes {
		if idx.Schema == "" {
			idx.Schema = "public"
		}
		if idx.Table == "" {
			idx.Table = "public.orders"
		}
		if idx.Method == "" {
			idx.Method = "btree"
		}
		s.Indexes[qualify(idx.Schema, idx.Name)] = idx
	}
	return s
}

func TestHasIndexOn(t *testing.T) {
	tests := []struct {
		name  string
		s     *Schema
		cols  []string
		want  bool
	}{
		{"exact match", buildSchema(&Index{Name: "i1", Columns: []string{"a"}}), []string{"a"}, true},
		{"prefix covers request (longer idx)", buildSchema(&Index{Name: "i1", Columns: []string{"a", "b", "c"}}), []string{"a"}, true},
		{"prefix covers two-col request", buildSchema(&Index{Name: "i1", Columns: []string{"a", "b", "c"}}), []string{"a", "b"}, true},
		{"fourth column mismatch", buildSchema(&Index{Name: "i1", Columns: []string{"a", "b", "c"}}), []string{"a", "b", "d"}, false},
		{"ordering matters", buildSchema(&Index{Name: "i1", Columns: []string{"b", "a"}}), []string{"a"}, false},
		{"shorter idx cannot cover longer request", buildSchema(&Index{Name: "i1", Columns: []string{"a"}}), []string{"a", "b"}, false},
		{"partial index skipped", buildSchema(&Index{Name: "i1", Columns: []string{"a"}, IsPartial: true}), []string{"a"}, false},
		{"non-btree skipped", buildSchema(&Index{Name: "i1", Columns: []string{"a"}, Method: "gin"}), []string{"a"}, false},
		{"empty schema", &Schema{}, []string{"a"}, false},
		{"empty cols returns false", buildSchema(&Index{Name: "i1", Columns: []string{"a"}}), nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.HasIndexOn("public.orders", tt.cols); got != tt.want {
				t.Errorf("HasIndexOn = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindIndexPrefixing(t *testing.T) {
	tests := []struct {
		name     string
		s        *Schema
		cols     []string
		wantName string // "" → nil expected
	}{
		{"single-col prefix of two-col request", buildSchema(&Index{Name: "i_a", Columns: []string{"a"}}), []string{"a", "b"}, "i_a"},
		{"longer prefix preferred over shorter", buildSchema(
			&Index{Name: "i_a", Columns: []string{"a"}},
			&Index{Name: "i_ab", Columns: []string{"a", "b"}},
		), []string{"a", "b", "c"}, "i_ab"},
		{"exact match returns nil (HasIndexOn handles it)", buildSchema(&Index{Name: "i_ab", Columns: []string{"a", "b"}}), []string{"a", "b"}, ""},
		{"longer index returns nil", buildSchema(&Index{Name: "i_abc", Columns: []string{"a", "b", "c"}}), []string{"a", "b"}, ""},
		{"different ordering returns nil", buildSchema(&Index{Name: "i_ba", Columns: []string{"b", "a"}}), []string{"a", "b", "c"}, ""},
		{"diverging prefix returns nil", buildSchema(&Index{Name: "i_ad", Columns: []string{"a", "d"}}), []string{"a", "b", "c"}, ""},
		{"single-col request skipped (no meaningful extension)", buildSchema(&Index{Name: "i_a", Columns: []string{"a"}}), []string{"a"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.s.FindIndexPrefixing("public.orders", tt.cols)
			if tt.wantName == "" {
				if got != nil {
					t.Errorf("expected nil, got index %q", got.Name)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected index %q, got nil", tt.wantName)
			}
			if got.Name != tt.wantName {
				t.Errorf("got index %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}

func TestIndexesOn(t *testing.T) {
	s := buildSchema(
		&Index{Name: "i1", Columns: []string{"a"}},
		&Index{Name: "i2", Columns: []string{"b"}},
	)
	got := s.IndexesOn("public.orders")
	if len(got) != 2 {
		t.Errorf("IndexesOn returned %d indexes, want 2", len(got))
	}
	if len(s.IndexesOn("public.nope")) != 0 {
		t.Errorf("unknown table should return empty slice")
	}
}

func TestFindTable(t *testing.T) {
	s := &Schema{
		Tables: map[string]*Table{
			"public.orders":     {Schema: "public", Name: "orders"},
			"archive.orders":    {Schema: "archive", Name: "orders"},
			"analytics.events":  {Schema: "analytics", Name: "events"},
		},
	}
	if got := s.FindTable("public.orders"); got == nil || got.Schema != "public" {
		t.Errorf("FQN lookup failed: %+v", got)
	}
	if got := s.FindTable("orders"); got == nil || got.Schema != "public" {
		t.Errorf("bare name should prefer public: %+v", got)
	}
	if got := s.FindTable("events"); got == nil || got.Schema != "analytics" {
		t.Errorf("bare name should resolve unique match: %+v", got)
	}
	// Ambiguous case: add public.events so bare "events" is ambiguous.
	s.Tables["public.events"] = &Table{Schema: "public", Name: "events"}
	if got := s.FindTable("events"); got == nil || got.Schema != "public" {
		t.Errorf("public should win over analytics: %+v", got)
	}
	delete(s.Tables, "public.events")
	s.Tables["archive.events"] = &Table{Schema: "archive", Name: "events"}
	if got := s.FindTable("events"); got != nil {
		t.Errorf("ambiguous non-public bare name should be nil, got %+v", got)
	}
}

func TestForeignKeysOn(t *testing.T) {
	s := &Schema{
		FKeys: []ForeignKey{
			{Schema: "public", Table: "orders", Name: "o_fk1", Columns: []string{"customer_id"}},
			{Schema: "public", Table: "orders", Name: "o_fk2", Columns: []string{"product_id"}},
			{Schema: "public", Table: "payments", Name: "p_fk1", Columns: []string{"order_id"}},
		},
	}
	if n := len(s.ForeignKeysOn("public.orders")); n != 2 {
		t.Errorf("ForeignKeysOn(orders) = %d, want 2", n)
	}
	if n := len(s.ForeignKeysOn("public.missing")); n != 0 {
		t.Errorf("unknown table should return empty slice")
	}
}

func TestFKColumnsWithoutIndex(t *testing.T) {
	s := buildSchemaWithFK(
		[]*Index{
			{Name: "orders_pkey", Table: "public.orders", Columns: []string{"id"}, IsPrimary: true, IsUnique: true},
			{Name: "orders_customer_idx", Table: "public.orders", Columns: []string{"customer_id"}},
			// no index on payments.order_id
		},
		[]ForeignKey{
			{Schema: "public", Table: "orders", Name: "orders_customer_fkey", Columns: []string{"customer_id"}, ReferencedSchema: "public", ReferencedTable: "customers", ReferencedColumns: []string{"id"}},
			{Schema: "public", Table: "payments", Name: "payments_order_fkey", Columns: []string{"order_id"}, ReferencedSchema: "public", ReferencedTable: "orders", ReferencedColumns: []string{"id"}},
		},
	)
	got := s.FKColumnsWithoutIndex()
	if len(got) != 1 {
		t.Fatalf("got %d uncovered FKs, want 1: %+v", len(got), got)
	}
	if got[0].FK.Name != "payments_order_fkey" {
		t.Errorf("uncovered FK = %q, want payments_order_fkey", got[0].FK.Name)
	}
}

// buildSchemaWithFK builds a minimal Schema with multiple tables and
// explicit FKs, so FKColumnsWithoutIndex tests aren't limited to
// buildSchema's single-table shape.
func buildSchemaWithFK(indexes []*Index, fks []ForeignKey) *Schema {
	s := &Schema{
		Tables:  map[string]*Table{},
		Indexes: map[string]*Index{},
	}
	for _, fk := range fks {
		s.Tables[qualify(fk.Schema, fk.Table)] = &Table{Schema: fk.Schema, Name: fk.Table}
		s.Tables[qualify(fk.ReferencedSchema, fk.ReferencedTable)] = &Table{Schema: fk.ReferencedSchema, Name: fk.ReferencedTable}
	}
	for _, idx := range indexes {
		if idx.Method == "" {
			idx.Method = "btree"
		}
		if idx.Schema == "" {
			// Derive from Table FQN.
			idx.Schema = "public"
		}
		s.Indexes[qualify(idx.Schema, idx.Name)] = idx
	}
	s.FKeys = fks
	return s
}
