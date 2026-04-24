package rules

import "testing"

func TestPlanningVsExecution(t *testing.T) {
	rule := &PlanningVsExecution{}

	t.Run("info when planning > execution and execution < 10ms", func(t *testing.T) {
		p := loadRuleFixture(t, "planning_vs_execution", "positive.json")
		f := rule.Check(newContext(p))
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if f[0].Severity != SeverityInfo {
			t.Errorf("severity = %v, want Info", f[0].Severity)
		}
		if f[0].NodeID != 0 {
			t.Errorf("NodeID = %d, want 0 (plan-level)", f[0].NodeID)
		}
	})

	t.Run("no finding when execution is slow", func(t *testing.T) {
		p := loadRuleFixture(t, "planning_vs_execution", "negative.json")
		if f := rule.Check(newContext(p)); len(f) != 0 {
			t.Errorf("got %d findings, want 0", len(f))
		}
	})
}
