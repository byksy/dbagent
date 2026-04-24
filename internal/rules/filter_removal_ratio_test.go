package rules

import "testing"

func TestFilterRemovalRatio(t *testing.T) {
	tests := []struct {
		name         string
		fixture      string
		wantCount    int
		wantSeverity Severity
	}{
		{"critical on high volume + 99%", "positive.json", 1, SeverityCritical},
		{"skipped when removed < minRows", "negative_tiny.json", 0, 0},
		{"skipped when no filter", "negative.json", 0, 0},
		// Regression for per-loop vs total normalisation: a nested-loop
		// inner scan reports per-loop rows, so without scaling by Loops
		// the rule would have understated the removal volume and landed
		// on Info instead of Critical.
		{"critical on nested-loop inner scan with Loops>1", "positive_nested_loop.json", 1, SeverityCritical},
	}
	rule := &FilterRemovalRatio{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := loadRuleFixture(t, "filter_removal_ratio", tc.fixture)
			findings := rule.Check(newContext(p))
			if len(findings) != tc.wantCount {
				t.Fatalf("got %d findings, want %d: %+v", len(findings), tc.wantCount, findings)
			}
			if tc.wantCount == 0 {
				return
			}
			if findings[0].Severity != tc.wantSeverity {
				t.Errorf("severity = %v, want %v", findings[0].Severity, tc.wantSeverity)
			}
			if findings[0].Suggested != "" {
				t.Errorf("diagnostic rule must not emit Suggested, got %q", findings[0].Suggested)
			}
		})
	}
}

func TestFilterRemovalRatio_TotalsReportedInEvidence(t *testing.T) {
	p := loadRuleFixture(t, "filter_removal_ratio", "positive_nested_loop.json")
	f := (&FilterRemovalRatio{}).Check(newContext(p))
	if len(f) != 1 {
		t.Fatalf("got %d findings, want 1", len(f))
	}
	// 500 per-loop × 1000 loops = 500,000 total removals.
	if got := f[0].Evidence["rows_removed"].(int64); got != 500_000 {
		t.Errorf("rows_removed = %d, want 500000 (loop-scaled)", got)
	}
	if got := f[0].Evidence["rows_kept"].(int64); got != 1_000 {
		t.Errorf("rows_kept = %d, want 1000 (loop-scaled)", got)
	}
}
