package rules

import (
	"strings"
	"testing"

	"github.com/byksy/dbagent/internal/schema"
)

// duplicateIndexSchema returns a schema with two overlapping indexes
// on orders (one is a duplicate of the other) plus a constraint
// index to verify the rule doesn't propose DROP on it.
func duplicateIndexSchema() *schema.Schema {
	return &schema.Schema{
		Tables: map[string]*schema.Table{
			"public.customers": {Schema: "public", Name: "customers"},
			"public.orders":    {Schema: "public", Name: "orders"},
		},
		Indexes: map[string]*schema.Index{
			"public.orders_pkey": {
				Schema: "public", Name: "orders_pkey", Table: "public.orders",
				Columns: []string{"id"}, IsPrimary: true, IsUnique: true, Method: "btree",
			},
			"public.orders_cust_status_idx": {
				Schema: "public", Name: "orders_cust_status_idx", Table: "public.orders",
				Columns: []string{"customer_id", "status"}, Method: "btree",
				Definition: "CREATE INDEX orders_cust_status_idx ON public.orders USING btree (customer_id, status)",
			},
			"public.orders_duplicate_cust_status_idx": {
				Schema: "public", Name: "orders_duplicate_cust_status_idx", Table: "public.orders",
				Columns: []string{"customer_id", "status"}, Method: "btree",
				Definition: "CREATE INDEX orders_duplicate_cust_status_idx ON public.orders USING btree (customer_id, status)",
			},
		},
	}
}

func TestDuplicateIndex(t *testing.T) {
	rule := &DuplicateIndex{}

	t.Run("keeps lexicographically smallest, drops the rest", func(t *testing.T) {
		p := loadRuleFixture(t, "duplicate_index", "positive.json")
		f := rule.Check(&RuleContext{Plan: p, Schema: duplicateIndexSchema()})
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if f[0].Severity != SeverityWarning {
			t.Errorf("severity = %v, want Warning", f[0].Severity)
		}
		// cust_status_idx < duplicate_cust_status_idx, so the former
		// is kept and the latter is dropped.
		if !strings.Contains(f[0].Suggested, "DROP INDEX public.orders_duplicate_cust_status_idx") {
			t.Errorf("Suggested should drop the later-named duplicate, got %q", f[0].Suggested)
		}
		if strings.Contains(f[0].Suggested, "DROP INDEX public.orders_cust_status_idx;") {
			t.Errorf("Suggested must not drop the keeper, got %q", f[0].Suggested)
		}
	})

	t.Run("drops N-1 entries in a 3-index duplicate group", func(t *testing.T) {
		s := duplicateIndexSchema()
		s.Indexes["public.orders_cust_status_idx3"] = &schema.Index{
			Schema: "public", Name: "orders_cust_status_idx3", Table: "public.orders",
			Columns: []string{"customer_id", "status"}, Method: "btree",
			Definition: "CREATE INDEX orders_cust_status_idx3 ON public.orders USING btree (customer_id, status)",
		}
		p := loadRuleFixture(t, "duplicate_index", "positive.json")
		f := rule.Check(&RuleContext{Plan: p, Schema: s})
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		// Expect two DROPs (the keeper is cust_status_idx, the two
		// others get proposed for drop).
		dropCount := strings.Count(f[0].Suggested, "DROP INDEX")
		if dropCount != 2 {
			t.Errorf("got %d DROP INDEX lines, want 2 for a 3-index duplicate group:\n%s", dropCount, f[0].Suggested)
		}
	})

	t.Run("silent when plan doesn't touch the affected table", func(t *testing.T) {
		p := loadRuleFixture(t, "duplicate_index", "negative.json")
		if f := rule.Check(&RuleContext{Plan: p, Schema: duplicateIndexSchema()}); len(f) != 0 {
			t.Errorf("expected 0 findings, got %d: %+v", len(f), f)
		}
	})

	t.Run("silent without schema", func(t *testing.T) {
		p := loadRuleFixture(t, "duplicate_index", "positive.json")
		if f := rule.Check(newContext(p)); len(f) != 0 {
			t.Errorf("expected 0 findings without schema, got %d", len(f))
		}
	})

	t.Run("pkey-only duplicate group still reports but omits Suggested", func(t *testing.T) {
		// Contrived: two primary keys on the same columns (shouldn't
		// actually happen in PG, but the safety path matters if a
		// bad schema export creates it).
		s := duplicateIndexSchema()
		idx2 := *s.Indexes["public.orders_pkey"]
		idx2.Name = "orders_pkey_clone"
		s.Indexes["public.orders_pkey_clone"] = &idx2
		// Remove the existing safe duplicates to isolate the scenario.
		delete(s.Indexes, "public.orders_cust_status_idx")
		delete(s.Indexes, "public.orders_duplicate_cust_status_idx")

		p := loadRuleFixture(t, "duplicate_index", "positive.json")
		f := rule.Check(&RuleContext{Plan: p, Schema: s})
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1", len(f))
		}
		if f[0].Suggested != "" {
			t.Errorf("Suggested should be empty for all-constraint duplicate, got %q", f[0].Suggested)
		}
	})
}
