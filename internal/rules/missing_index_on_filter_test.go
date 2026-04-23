package rules

import (
	"strings"
	"testing"
)

func TestMissingIndexOnFilter(t *testing.T) {
	rule := &MissingIndexOnFilter{}

	t.Run("fires with CREATE INDEX on positive", func(t *testing.T) {
		p := loadRuleFixture(t, "missing_index_on_filter", "positive.json")
		f := rule.Check(p)
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
		f := rule.Check(p)
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if f[0].Suggested != "" {
			t.Errorf("expected empty Suggested for unparseable filter, got %q", f[0].Suggested)
		}
	})

	t.Run("skips index scans without filter", func(t *testing.T) {
		p := loadRuleFixture(t, "missing_index_on_filter", "negative.json")
		if f := rule.Check(p); len(f) != 0 {
			t.Errorf("got %d findings, want 0: %+v", len(f), f)
		}
	})
}
