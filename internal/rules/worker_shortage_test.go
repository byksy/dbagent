package rules

import "testing"

func TestWorkerShortage(t *testing.T) {
	rule := &WorkerShortage{}

	t.Run("warning when shortfall >= 2 workers", func(t *testing.T) {
		p := loadRuleFixture(t, "worker_shortage", "positive.json")
		f := rule.Check(newContext(p))
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if f[0].Severity != SeverityWarning {
			t.Errorf("severity = %v, want Warning", f[0].Severity)
		}
		if f[0].Evidence["shortfall"].(int) != 2 {
			t.Errorf("shortfall = %v, want 2", f[0].Evidence["shortfall"])
		}
	})

	t.Run("no finding when all workers launched", func(t *testing.T) {
		p := loadRuleFixture(t, "worker_shortage", "negative.json")
		if f := rule.Check(newContext(p)); len(f) != 0 {
			t.Errorf("got %d findings, want 0: %+v", len(f), f)
		}
	})
}
