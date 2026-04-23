package rules

import (
	"strings"
	"testing"
)

func TestSortSpilled(t *testing.T) {
	rule := &SortSpilled{}

	t.Run("warning on disk-spilled sort under 1GB", func(t *testing.T) {
		p := loadRuleFixture(t, "sort_spilled", "positive.json")
		f := rule.Check(p)
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if f[0].Severity != SeverityWarning {
			t.Errorf("severity = %v, want Warning", f[0].Severity)
		}
		if !strings.Contains(f[0].Suggested, "SET LOCAL work_mem") {
			t.Errorf("suggested = %q, want SET LOCAL work_mem", f[0].Suggested)
		}
		meta := f[0].SuggestedMeta
		if meta["kind"] != "set_work_mem" {
			t.Errorf("SuggestedMeta.kind = %v", meta["kind"])
		}
	})

	t.Run("no finding on in-memory sort", func(t *testing.T) {
		p := loadRuleFixture(t, "sort_spilled", "negative.json")
		if f := rule.Check(p); len(f) != 0 {
			t.Errorf("got %d findings, want 0: %+v", len(f), f)
		}
	})
}
