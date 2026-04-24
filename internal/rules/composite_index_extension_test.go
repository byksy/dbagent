package rules

import (
	"strings"
	"testing"

	"github.com/byksy/dbagent/internal/schema"
)

// compositeExtensionSchema mirrors what testdata/schemas/small.json
// exposes for public.customers — a region index that the positive
// fixture scans, plus a trailing filter on name that the rule
// proposes to fold into the index.
func compositeExtensionSchema() *schema.Schema {
	return &schema.Schema{
		Tables: map[string]*schema.Table{
			"public.customers": {Schema: "public", Name: "customers"},
		},
		Indexes: map[string]*schema.Index{
			"public.customers_region_idx": {
				Schema: "public", Name: "customers_region_idx", Table: "public.customers",
				Columns: []string{"region"}, Method: "btree",
				Definition: "CREATE INDEX customers_region_idx ON public.customers USING btree (region)",
			},
		},
	}
}

func TestCompositeIndexExtension(t *testing.T) {
	rule := &CompositeIndexExtension{}

	t.Run("fires on index scan with trailing filter", func(t *testing.T) {
		p := loadRuleFixture(t, "composite_index_extension", "positive.json")
		f := rule.Check(&RuleContext{Plan: p, Schema: compositeExtensionSchema()})
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if f[0].Severity != SeverityWarning {
			t.Errorf("severity = %v, want Warning", f[0].Severity)
		}
		if !strings.Contains(f[0].Suggested, "DROP INDEX public.customers_region_idx") {
			t.Errorf("Suggested should DROP existing, got %q", f[0].Suggested)
		}
		if !strings.Contains(f[0].Suggested, "CREATE INDEX ON public.customers (region, name)") {
			t.Errorf("Suggested should CREATE extended index, got %q", f[0].Suggested)
		}
	})

	t.Run("silent when no trailing filter", func(t *testing.T) {
		p := loadRuleFixture(t, "composite_index_extension", "negative.json")
		if f := rule.Check(&RuleContext{Plan: p, Schema: compositeExtensionSchema()}); len(f) != 0 {
			t.Errorf("expected 0 findings, got %d: %+v", len(f), f)
		}
	})

	t.Run("silent when existing index is unique/primary", func(t *testing.T) {
		s := compositeExtensionSchema()
		idx := s.Indexes["public.customers_region_idx"]
		idx.IsUnique = true
		p := loadRuleFixture(t, "composite_index_extension", "positive.json")
		if f := rule.Check(&RuleContext{Plan: p, Schema: s}); len(f) != 0 {
			t.Errorf("expected 0 findings on unique index, got %d", len(f))
		}
	})

	t.Run("silent without schema", func(t *testing.T) {
		p := loadRuleFixture(t, "composite_index_extension", "positive.json")
		if f := rule.Check(newContext(p)); len(f) != 0 {
			t.Errorf("expected 0 findings without schema, got %d", len(f))
		}
	})
}

func TestMergeColumns(t *testing.T) {
	tests := []struct {
		current, extra, want []string
	}{
		{[]string{"a"}, []string{"b"}, []string{"a", "b"}},
		{[]string{"a", "b"}, []string{"b", "c"}, []string{"a", "b", "c"}},
		{[]string{"a"}, []string{"a"}, []string{"a"}},
		{[]string{}, []string{"x", "y"}, []string{"x", "y"}},
	}
	for _, tt := range tests {
		got := mergeColumns(tt.current, tt.extra)
		if len(got) != len(tt.want) {
			t.Errorf("mergeColumns(%v, %v) = %v, want %v", tt.current, tt.extra, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("mergeColumns(%v, %v) = %v, want %v", tt.current, tt.extra, got, tt.want)
				break
			}
		}
	}
}
