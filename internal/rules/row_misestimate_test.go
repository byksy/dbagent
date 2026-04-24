package rules

import (
	"strings"
	"testing"
	"time"

	"github.com/byksy/dbagent/internal/schema"
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
			if !strings.Contains(findings[0].Suggested, "ANALYZE") {
				t.Errorf("expected ANALYZE suggestion, got %q", findings[0].Suggested)
			}
			if meta := findings[0].SuggestedMeta; meta == nil || meta["kind"] != "analyze" {
				t.Errorf("suggested meta missing or wrong kind: %+v", meta)
			}
		})
	}
}

// Schema-aware augmentation: when schema shows the table was
// ANALYZEd more than a week ago, the rule bumps severity one tier
// and mentions the age in the message.
func TestRowMisestimate_StaleAnalyzeBumpsSeverity(t *testing.T) {
	p := loadRuleFixture(t, "row_misestimate", "positive_100x.json")
	s := &schema.Schema{
		Tables: map[string]*schema.Table{
			"public.rule_orders": {
				Schema: "public", Name: "rule_orders",
				LastAnalyzed: time.Now().Add(-10 * 24 * time.Hour),
			},
		},
	}
	f := (&RowMisestimate{}).Check(&RuleContext{Plan: p, Schema: s})
	if len(f) != 1 {
		t.Fatalf("got %d findings, want 1", len(f))
	}
	if f[0].Severity != SeverityCritical {
		t.Errorf("severity = %v, want Critical (bumped from Warning)", f[0].Severity)
	}
	if !strings.Contains(f[0].Message, "Last ANALYZE was") {
		t.Errorf("message should mention last ANALYZE age: %q", f[0].Message)
	}
	if _, ok := f[0].Evidence["last_analyzed"]; !ok {
		t.Errorf("evidence should include last_analyzed")
	}
}

// Fresh ANALYZE on the same plan does not bump severity.
func TestRowMisestimate_FreshAnalyzeNoBump(t *testing.T) {
	p := loadRuleFixture(t, "row_misestimate", "positive_100x.json")
	s := &schema.Schema{
		Tables: map[string]*schema.Table{
			"public.rule_orders": {
				Schema: "public", Name: "rule_orders",
				LastAnalyzed: time.Now().Add(-1 * time.Hour),
			},
		},
	}
	f := (&RowMisestimate{}).Check(&RuleContext{Plan: p, Schema: s})
	if len(f) != 1 {
		t.Fatalf("got %d findings, want 1", len(f))
	}
	if f[0].Severity != SeverityWarning {
		t.Errorf("severity = %v, want Warning (unchanged by fresh ANALYZE)", f[0].Severity)
	}
}
