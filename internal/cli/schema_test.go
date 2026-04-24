package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/byksy/dbagent/internal/schema"
)

// fakeSchema returns a minimal Schema suitable for overview / JSON
// round-trip tests. Keep it small but non-trivial so the renderers
// exercise multiple entries per section.
func fakeSchema() *schema.Schema {
	return &schema.Schema{
		Meta: schema.Meta{
			ExportedAt:    time.Now().UTC(),
			Database:      "dbagent_dev",
			ServerVersion: "17.2",
		},
		Tables: map[string]*schema.Table{
			"public.customers": {
				Schema: "public", Name: "customers", RowEstimate: 5000, SizeBytes: 640 * 1024,
			},
			"public.orders": {
				Schema: "public", Name: "orders", RowEstimate: 500000, SizeBytes: 52 * 1024 * 1024,
			},
		},
		Indexes: map[string]*schema.Index{
			"public.customers_pkey": {
				Schema: "public", Name: "customers_pkey", Table: "public.customers",
				Columns: []string{"id"}, IsPrimary: true, IsUnique: true, Method: "btree", SizeBytes: 240 * 1024,
			},
			"public.orders_pkey": {
				Schema: "public", Name: "orders_pkey", Table: "public.orders",
				Columns: []string{"id"}, IsPrimary: true, IsUnique: true, Method: "btree", SizeBytes: 12 * 1024 * 1024,
			},
		},
		FKeys: []schema.ForeignKey{
			{
				Schema: "public", Table: "orders", Name: "orders_customer_fkey",
				Columns:           []string{"customer_id"},
				ReferencedSchema:  "public",
				ReferencedTable:   "customers",
				ReferencedColumns: []string{"id"},
			},
		},
	}
}

func TestWriteSchemaOverview_ContainsSections(t *testing.T) {
	var buf bytes.Buffer
	writeSchemaOverview(&buf, fakeSchema())
	s := buf.String()
	for _, want := range []string{
		"Database: dbagent_dev",
		"Tables (2 total)",
		"Indexes (2 total)",
		"Foreign keys (1 total)",
		"customers",
		"orders_pkey",
		"customer_id",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("overview missing %q\n---output---\n%s", want, s)
		}
	}
}

func TestSchemaExport_JSONRoundTrip(t *testing.T) {
	orig := fakeSchema()
	var buf bytes.Buffer
	if err := orig.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	// Validate via raw unmarshal first: the exported document must be
	// well-formed JSON with the expected top-level keys.
	var raw map[string]any
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("JSON is not parseable: %v", err)
	}
	for _, key := range []string{"meta", "tables", "indexes", "foreign_keys"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("exported JSON missing top-level key %q", key)
		}
	}

	// Round-trip through LoadJSON so the typed layer agrees.
	got, err := schema.LoadJSON(&buf)
	if err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}
	if got.Meta.Database != "dbagent_dev" {
		t.Errorf("database = %q", got.Meta.Database)
	}
	if len(got.Tables) != 2 || len(got.Indexes) != 2 || len(got.FKeys) != 1 {
		t.Errorf("counts drift: tables=%d indexes=%d fkeys=%d", len(got.Tables), len(got.Indexes), len(got.FKeys))
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 kB"},
		{2048, "2.0 kB"},
		{640 * 1024, "640.0 kB"},
		{1024 * 1024, "1.0 MB"},
		{52 * 1024 * 1024, "52.0 MB"},
		{2 * 1024 * 1024 * 1024, "2.0 GB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
