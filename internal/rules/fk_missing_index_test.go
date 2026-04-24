package rules

// Test pairing note: fk_missing_index needs BOTH a plan and a schema.
// For unit tests we build minimal schemas inline (see testSchema) so
// the assertions stay deterministic and don't depend on the capture
// scripts having run. The schema mirrors testdata/schemas/small.json
// so CLI-level smoke tests exercise the same shape.

import (
	"testing"

	"github.com/byksy/dbagent/internal/schema"
)

// testSchema mirrors testdata/schemas/small.json: public.customers
// and public.orders, with orders.customer_id having a FK into
// customers.id but NO supporting index. Unit tests share this
// exact shape with the acceptance-criteria smoke tests so a change
// in one is caught in the other.
func testSchema() *schema.Schema {
	return &schema.Schema{
		Tables: map[string]*schema.Table{
			"public.customers": {Schema: "public", Name: "customers"},
			"public.orders":    {Schema: "public", Name: "orders"},
		},
		Indexes: map[string]*schema.Index{
			"public.customers_pkey": {
				Schema: "public", Name: "customers_pkey", Table: "public.customers",
				Columns: []string{"id"}, IsPrimary: true, IsUnique: true, Method: "btree",
			},
			"public.orders_pkey": {
				Schema: "public", Name: "orders_pkey", Table: "public.orders",
				Columns: []string{"id"}, IsPrimary: true, IsUnique: true, Method: "btree",
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

func TestFKMissingIndex_FiresOnTouchedTable(t *testing.T) {
	// positive fixture scans "orders"; the schema's FK on that
	// table lacks an index, so one finding anchored at the Seq Scan
	// is expected.
	p := loadRuleFixture(t, "fk_missing_index", "positive.json")
	ctx := &RuleContext{Plan: p, Schema: testSchema()}
	findings := (&FKMissingIndex{}).Check(ctx)
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != SeverityWarning {
		t.Errorf("severity = %v, want Warning", f.Severity)
	}
	if f.Evidence["fk_name"] != "orders_customer_fkey" {
		t.Errorf("fk_name = %v, want orders_customer_fkey", f.Evidence["fk_name"])
	}
	if f.Suggested != "CREATE INDEX ON public.orders (customer_id);" {
		t.Errorf("Suggested = %q", f.Suggested)
	}
	if f.NodeID == 0 {
		t.Errorf("NodeID should anchor to a scan node, got 0")
	}
}

func TestFKMissingIndex_SilentWhenSchemaNil(t *testing.T) {
	p := loadRuleFixture(t, "fk_missing_index", "positive.json")
	if f := (&FKMissingIndex{}).Check(&RuleContext{Plan: p}); len(f) != 0 {
		t.Errorf("expected 0 findings without schema, got %d", len(f))
	}
}

func TestFKMissingIndex_SilentWhenPlanDoesNotTouchTable(t *testing.T) {
	// negative fixture scans "customers"; the uncovered FK is on
	// orders, which the plan never touches, so no finding emerges.
	p := loadRuleFixture(t, "fk_missing_index", "negative.json")
	findings := (&FKMissingIndex{}).Check(&RuleContext{Plan: p, Schema: testSchema()})
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}
