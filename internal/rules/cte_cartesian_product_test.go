package rules

import "testing"

func TestCTECartesianProduct(t *testing.T) {
	rule := &CTECartesianProduct{}

	t.Run("fires on rescanned CTE", func(t *testing.T) {
		p := loadRuleFixture(t, "cte_cartesian_product", "positive.json")
		f := rule.Check(newContext(p))
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if f[0].Evidence["cte_name"] != "recent_orders" {
			t.Errorf("cte_name = %v, want recent_orders", f[0].Evidence["cte_name"])
		}
		// 50 loops × (200 kept + 10 removed) = 10,500 — over the
		// warning threshold, below critical (100k).
		if f[0].Severity != SeverityWarning {
			t.Errorf("severity = %v, want Warning", f[0].Severity)
		}
	})

	t.Run("silent when CTE used once", func(t *testing.T) {
		p := loadRuleFixture(t, "cte_cartesian_product", "negative.json")
		if f := rule.Check(newContext(p)); len(f) != 0 {
			t.Errorf("expected 0 findings, got %d: %+v", len(f), f)
		}
	})
}
