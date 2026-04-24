package schema

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWriteAndLoadJSON_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	orig := &Schema{
		Meta: Meta{
			ExportedAt:     now,
			Database:       "postgres",
			ServerVersion:  "17.2",
			DBAgentVersion: "v0.4.0-test",
		},
		Tables: map[string]*Table{
			"public.orders": {
				Schema: "public", Name: "orders", RowEstimate: 42, SizeBytes: 1024,
				Columns: []Column{
					{Name: "id", DataType: "integer", NotNull: true, Position: 1},
				},
			},
		},
		Indexes: map[string]*Index{
			"public.orders_pkey": {
				Schema: "public", Name: "orders_pkey", Table: "public.orders",
				Columns: []string{"id"}, IsPrimary: true, IsUnique: true, Method: "btree",
			},
		},
		FKeys: []ForeignKey{
			{Schema: "public", Table: "orders", Name: "orders_customer_fkey",
				Columns: []string{"customer_id"}, ReferencedSchema: "public",
				ReferencedTable: "customers", ReferencedColumns: []string{"id"}},
		},
	}

	var buf bytes.Buffer
	if err := orig.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	got, err := LoadJSON(&buf)
	if err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}
	if got.Meta.Database != "postgres" {
		t.Errorf("Database = %q", got.Meta.Database)
	}
	if !got.Meta.ExportedAt.Equal(now) {
		t.Errorf("ExportedAt drift: got %v want %v", got.Meta.ExportedAt, now)
	}
	if tbl := got.Tables["public.orders"]; tbl == nil || tbl.RowEstimate != 42 {
		t.Errorf("missing table or wrong RowEstimate: %+v", tbl)
	}
	if len(got.FKeys) != 1 || got.FKeys[0].Name != "orders_customer_fkey" {
		t.Errorf("fkeys round-trip mismatch: %+v", got.FKeys)
	}
}

func TestLoadJSON_StaleReturnsSchemaAndError(t *testing.T) {
	stale := &Schema{
		Meta: Meta{
			ExportedAt: time.Now().Add(-48 * time.Hour),
			Database:   "postgres",
		},
		Tables:  map[string]*Table{},
		Indexes: map[string]*Index{},
	}
	var buf bytes.Buffer
	if err := stale.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	got, err := LoadJSON(&buf)
	if !errors.Is(err, ErrStaleSchema) {
		t.Fatalf("expected ErrStaleSchema, got %v", err)
	}
	if got == nil {
		t.Fatalf("schema should still be returned alongside ErrStaleSchema")
	}
	if !got.IsStale() {
		t.Errorf("IsStale should be true")
	}
}

func TestLoadJSON_FreshHasNoError(t *testing.T) {
	fresh := &Schema{
		Meta: Meta{ExportedAt: time.Now(), Database: "x"},
	}
	var buf bytes.Buffer
	if err := fresh.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadJSON(&buf); err != nil {
		t.Errorf("fresh schema should not error: %v", err)
	}
}

func TestLoadJSON_Malformed(t *testing.T) {
	_, err := LoadJSON(strings.NewReader("{not json"))
	if err == nil {
		t.Fatalf("expected decode error")
	}
	if errors.Is(err, ErrStaleSchema) {
		t.Errorf("malformed input should not be reported as stale")
	}
}

func TestIsStale_ZeroTime(t *testing.T) {
	s := &Schema{}
	if s.IsStale() {
		t.Errorf("zero ExportedAt must not be stale")
	}
}
