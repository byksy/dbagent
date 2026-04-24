package rules

import (
	"strings"
	"testing"
)

func TestTableBloat(t *testing.T) {
	rule := &TableBloat{}

	t.Run("fires when blocks-per-row is disproportionate", func(t *testing.T) {
		p := loadRuleFixture(t, "table_bloat", "positive.json")
		f := rule.Check(newContext(p))
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if !strings.Contains(f[0].Suggested, "VACUUM (ANALYZE)") {
			t.Errorf("Suggested should be VACUUM (ANALYZE), got %q", f[0].Suggested)
		}
		if strings.Contains(f[0].Suggested, "FULL") {
			t.Errorf("Suggested must not include VACUUM FULL")
		}
		if f[0].SuggestedMeta["mode"] != "standard" {
			t.Errorf("mode = %v, want standard", f[0].SuggestedMeta["mode"])
		}
	})

	t.Run("silent on healthy scans", func(t *testing.T) {
		p := loadRuleFixture(t, "table_bloat", "negative.json")
		if f := rule.Check(newContext(p)); len(f) != 0 {
			t.Errorf("expected 0 findings, got %d: %+v", len(f), f)
		}
	})
}
