package rules

import (
	"strings"
	"testing"

	"github.com/byksy/dbagent/internal/schema"
)

func TestMissingIndexOnFilter(t *testing.T) {
	rule := &MissingIndexOnFilter{}

	t.Run("fires with CREATE INDEX on positive", func(t *testing.T) {
		p := loadRuleFixture(t, "missing_index_on_filter", "positive.json")
		f := rule.Check(newContext(p))
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if f[0].Severity != SeverityCritical {
			t.Errorf("severity = %v, want Critical", f[0].Severity)
		}
		if !strings.Contains(f[0].Suggested, "CREATE INDEX ON rule_orders (status)") {
			t.Errorf("suggested = %q, want CREATE INDEX on rule_orders(status)", f[0].Suggested)
		}
		meta := f[0].SuggestedMeta
		if meta["kind"] != "create_index" {
			t.Errorf("SuggestedMeta.kind = %v, want create_index", meta["kind"])
		}
	})

	t.Run("fires without Suggested when filter is unparseable", func(t *testing.T) {
		p := loadRuleFixture(t, "missing_index_on_filter", "positive_unparseable_filter.json")
		f := rule.Check(newContext(p))
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if f[0].Suggested != "" {
			t.Errorf("expected empty Suggested for unparseable filter, got %q", f[0].Suggested)
		}
	})

	t.Run("skips index scans without filter", func(t *testing.T) {
		p := loadRuleFixture(t, "missing_index_on_filter", "negative.json")
		if f := rule.Check(newContext(p)); len(f) != 0 {
			t.Errorf("got %d findings, want 0: %+v", len(f), f)
		}
	})

	// Schema-aware: an existing index covering the filter column
	// suppresses the rule. Filter volume is still high, but the
	// prescription would be wrong — the index already exists.
	t.Run("schema with matching index suppresses finding", func(t *testing.T) {
		p := loadRuleFixture(t, "missing_index_on_filter", "positive_with_existing_index.json")
		s := &schema.Schema{
			Tables: map[string]*schema.Table{
				"public.orders": {Schema: "public", Name: "orders"},
			},
			Indexes: map[string]*schema.Index{
				"public.orders_status_idx": {
					Schema: "public", Name: "orders_status_idx", Table: "public.orders",
					Columns: []string{"status"}, Method: "btree",
				},
			},
		}
		f := rule.Check(&RuleContext{Plan: p, Schema: s})
		if len(f) != 0 {
			t.Errorf("expected 0 findings with existing index, got %d: %+v", len(f), f)
		}
	})

	// Schema-aware: a shorter prefix index exists → suggest extending
	// it rather than creating a duplicate.
	t.Run("schema with prefix index suggests extension", func(t *testing.T) {
		p := loadRuleFixture(t, "missing_index_on_filter", "positive_with_prefix_index.json")
		s := &schema.Schema{
			Tables: map[string]*schema.Table{
				"public.orders": {Schema: "public", Name: "orders"},
			},
			Indexes: map[string]*schema.Index{
				"public.orders_status_idx": {
					Schema: "public", Name: "orders_status_idx", Table: "public.orders",
					Columns: []string{"status"}, Method: "btree",
					Definition: "CREATE INDEX orders_status_idx ON public.orders USING btree (status)",
				},
			},
		}
		f := rule.Check(&RuleContext{Plan: p, Schema: s})
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1", len(f))
		}
		if !strings.Contains(f[0].Suggested, "DROP INDEX public.orders_status_idx") {
			t.Errorf("expected DROP of existing index, got %q", f[0].Suggested)
		}
		if !strings.Contains(f[0].Suggested, "CREATE INDEX ON orders (status, region)") {
			t.Errorf("expected composite CREATE INDEX, got %q", f[0].Suggested)
		}
		if f[0].SuggestedMeta["kind"] != "extend_index" {
			t.Errorf("kind = %v, want extend_index", f[0].SuggestedMeta["kind"])
		}
	})

	// Schema absent: rule behaves like Stage 3 but tags the message
	// so operators know the suggestion wasn't verified.
	t.Run("offline mode annotates message", func(t *testing.T) {
		p := loadRuleFixture(t, "missing_index_on_filter", "positive.json")
		f := rule.Check(newContext(p))
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1", len(f))
		}
		if !strings.Contains(f[0].Message, "schema not available") {
			t.Errorf("offline message should note missing schema: %q", f[0].Message)
		}
	})

	// Regression: inner scan of a nested loop reports per-loop counts;
	// message and evidence must report the loop-scaled total so they
	// don't undercount versus the severity calculation.
	t.Run("reports loop-scaled totals for nested-loop inner", func(t *testing.T) {
		p := loadRuleFixture(t, "missing_index_on_filter", "positive_nested_loop.json")
		f := rule.Check(newContext(p))
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1", len(f))
		}
		// 500 per-loop × 1000 loops = 500,000 total removals.
		if got := f[0].Evidence["rows_removed"].(int64); got != 500_000 {
			t.Errorf("rows_removed = %d, want 500000 (loop-scaled)", got)
		}
		if !strings.Contains(f[0].Message, "500000 rows") {
			t.Errorf("message should report 500000 rows (loop-scaled): %q", f[0].Message)
		}
		if f[0].Severity != SeverityCritical {
			t.Errorf("severity = %v, want Critical", f[0].Severity)
		}
	})
}
