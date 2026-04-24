package rules

import "testing"

func TestNetworkOverhead(t *testing.T) {
	rule := &NetworkOverhead{}

	t.Run("fires on wide result set", func(t *testing.T) {
		p := loadRuleFixture(t, "network_overhead", "positive.json")
		f := rule.Check(newContext(p))
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		// 500,000 rows × 256 bytes = 128,000,000 bytes (~122 MB) →
		// Warning tier (>=100 MB, <1 GB).
		if f[0].Severity != SeverityWarning {
			t.Errorf("severity = %v, want Warning", f[0].Severity)
		}
		if f[0].NodeID != 0 {
			t.Errorf("NodeID = %d, want 0 (plan-level)", f[0].NodeID)
		}
	})

	t.Run("silent on small query", func(t *testing.T) {
		p := loadRuleFixture(t, "network_overhead", "negative.json")
		if f := rule.Check(newContext(p)); len(f) != 0 {
			t.Errorf("expected 0 findings, got %d: %+v", len(f), f)
		}
	})
}
