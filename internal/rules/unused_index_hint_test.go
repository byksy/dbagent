package rules

import (
	"strings"
	"testing"

	"github.com/byksy/dbagent/internal/schema"
)

// unusedIndexSchema is shared across the test cases. It has one
// non-constraint index with zero scans and another with non-zero
// scans (to avoid the "Stage 5 cold-start" guard muting the rule).
func unusedIndexSchema() *schema.Schema {
	return &schema.Schema{
		Tables: map[string]*schema.Table{
			"public.customers": {Schema: "public", Name: "customers"},
			"public.orders":    {Schema: "public", Name: "orders"},
		},
		Indexes: map[string]*schema.Index{
			"public.orders_pkey": {
				Schema: "public", Name: "orders_pkey", Table: "public.orders",
				Columns: []string{"id"}, IsPrimary: true, IsUnique: true, Method: "btree",
				Scans: 512,
			},
			"public.orders_stale_idx": {
				Schema: "public", Name: "orders_stale_idx", Table: "public.orders",
				Columns: []string{"legacy_flag"}, Method: "btree",
				Scans: 0,
			},
			"public.customers_pkey": {
				Schema: "public", Name: "customers_pkey", Table: "public.customers",
				Columns: []string{"id"}, IsPrimary: true, IsUnique: true, Method: "btree",
				Scans: 5,
			},
		},
	}
}

func TestUnusedIndexHint(t *testing.T) {
	rule := &UnusedIndexHint{}

	t.Run("fires on touched table with zero-scan non-constraint index", func(t *testing.T) {
		p := loadRuleFixture(t, "unused_index_hint", "positive.json")
		f := rule.Check(&RuleContext{Plan: p, Schema: unusedIndexSchema()})
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if f[0].Severity != SeverityInfo {
			t.Errorf("severity = %v, want Info", f[0].Severity)
		}
		names, _ := f[0].Evidence["unused_indexes"].([]string)
		if len(names) != 1 || names[0] != "orders_stale_idx" {
			t.Errorf("unused_indexes = %v, want [orders_stale_idx]", names)
		}
		if f[0].Suggested != "" {
			t.Errorf("rule must not emit DROP INDEX, got Suggested = %q", f[0].Suggested)
		}
	})

	t.Run("silent when touched table has no unused indexes", func(t *testing.T) {
		p := loadRuleFixture(t, "unused_index_hint", "negative.json")
		if f := rule.Check(&RuleContext{Plan: p, Schema: unusedIndexSchema()}); len(f) != 0 {
			t.Errorf("expected 0 findings, got %d: %+v", len(f), f)
		}
	})

	t.Run("silent on legacy schema export where all Scans are zero", func(t *testing.T) {
		// Simulate a schema JSON from before Scans existed: every
		// index reports Scans=0. We refuse to fire because we can't
		// distinguish "unused" from "field not populated".
		s := unusedIndexSchema()
		for _, idx := range s.Indexes {
			idx.Scans = 0
		}
		p := loadRuleFixture(t, "unused_index_hint", "positive.json")
		if f := rule.Check(&RuleContext{Plan: p, Schema: s}); len(f) != 0 {
			t.Errorf("expected silence on legacy schema, got %d findings", len(f))
		}
	})

	t.Run("silent without schema", func(t *testing.T) {
		p := loadRuleFixture(t, "unused_index_hint", "positive.json")
		if f := rule.Check(newContext(p)); len(f) != 0 {
			t.Errorf("expected 0 findings without schema, got %d", len(f))
		}
	})

	t.Run("message mentions no-drop policy", func(t *testing.T) {
		p := loadRuleFixture(t, "unused_index_hint", "positive.json")
		f := rule.Check(&RuleContext{Plan: p, Schema: unusedIndexSchema()})
		if len(f) != 1 || !strings.Contains(f[0].Message, "does not generate DROP INDEX") {
			t.Errorf("message should warn against auto-drop, got %q", f[0].Message)
		}
	})
}
