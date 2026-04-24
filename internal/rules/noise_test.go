package rules

import "testing"

// TestRules_NoisePlanSilent guards the noise gates added after Stage 3
// polish: a sub-millisecond plan with a tiny filter should not cause
// any of the eight built-in rules to fire. If a new rule is added
// without a similar gate, this test is the canary that catches it.
func TestRules_NoisePlanSilent(t *testing.T) {
	p := loadRuleFixture(t, "_noise", "small_seq_scan.json")
	findings := Run(p, Default())
	if len(findings) != 0 {
		t.Errorf("expected 0 findings on noise plan, got %d:\n%+v", len(findings), findings)
	}
}
