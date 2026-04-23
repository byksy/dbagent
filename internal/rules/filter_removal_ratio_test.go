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
	}
	rule := &FilterRemovalRatio{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := loadRuleFixture(t, "filter_removal_ratio", tc.fixture)
			findings := rule.Check(p)
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
