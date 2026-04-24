package rules

import (
	"testing"

	"github.com/byksy/dbagent/internal/plan"
)

func TestMemoizeOpportunity(t *testing.T) {
	rule := &MemoizeOpportunity{}

	t.Run("fires on repeated-key nested loop on PG 14+", func(t *testing.T) {
		p := loadRuleFixture(t, "memoize_opportunity", "positive.json")
		f := rule.Check(newContext(p))
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if f[0].Severity != SeverityInfo {
			t.Errorf("severity = %v, want Info", f[0].Severity)
		}
		if f[0].Suggested != "ANALYZE trips;" {
			t.Errorf("suggested = %q, want ANALYZE trips;", f[0].Suggested)
		}
		if f[0].Evidence["inner_node_type"] != "Index Scan" {
			t.Errorf("inner_node_type = %v", f[0].Evidence["inner_node_type"])
		}
	})

	t.Run("silent on non-nested-loop join", func(t *testing.T) {
		p := loadRuleFixture(t, "memoize_opportunity", "negative_not_nested_loop.json")
		if f := rule.Check(newContext(p)); len(f) != 0 {
			t.Errorf("expected 0 findings, got %d: %+v", len(f), f)
		}
	})

	t.Run("silent when Memoize already present", func(t *testing.T) {
		p := loadRuleFixture(t, "memoize_opportunity", "negative_with_memoize.json")
		if f := rule.Check(newContext(p)); len(f) != 0 {
			t.Errorf("expected 0 findings, got %d: %+v", len(f), f)
		}
	})
}

func TestPlanServerMajorVersion(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]string
		want     int
	}{
		{"17.2", map[string]string{"server_version": "17.2"}, 17},
		{"16.4 with platform", map[string]string{"server_version": "16.4 (Debian 16.4-1)"}, 16},
		{"17", map[string]string{"server_version": "17"}, 17},
		{"unknown empty", map[string]string{}, 0},
		{"nil settings", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planServerMajorVersion(&plan.Plan{Settings: tt.settings})
			if got != tt.want {
				t.Errorf("version = %d, want %d", got, tt.want)
			}
		})
	}
}
