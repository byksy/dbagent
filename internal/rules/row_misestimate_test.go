package rules

import (
	"strings"
	"testing"
)

func TestRowMisestimate(t *testing.T) {
	tests := []struct {
		name         string
		fixture      string
		wantCount    int
		wantSeverity Severity
	}{
		{"warning on 100x under-estimate", "positive_100x.json", 1, SeverityWarning},
		{"critical on 1000x under-estimate", "positive_1000x.json", 1, SeverityCritical},
		{"no finding on accurate estimate", "negative.json", 0, 0},
	}
	rule := &RowMisestimate{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := loadRuleFixture(t, "row_misestimate", tc.fixture)
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
			if !strings.Contains(findings[0].Suggested, "ANALYZE") {
				t.Errorf("expected ANALYZE suggestion, got %q", findings[0].Suggested)
			}
			if meta := findings[0].SuggestedMeta; meta == nil || meta["kind"] != "analyze" {
				t.Errorf("suggested meta missing or wrong kind: %+v", meta)
			}
		})
	}
}
