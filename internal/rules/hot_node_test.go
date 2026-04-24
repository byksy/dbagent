package rules

import "testing"

func TestHotNode(t *testing.T) {
	tests := []struct {
		name         string
		fixture      string
		wantCount    int
		wantSeverity Severity
		wantNodeID   int
	}{
		{"fires critical on dominant seq scan", "positive.json", 1, SeverityCritical, 2},
		{"no finding on balanced nested loop", "negative.json", 0, 0, 0},
	}
	rule := &HotNode{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := loadRuleFixture(t, "hot_node", tc.fixture)
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
			if findings[0].NodeID != tc.wantNodeID {
				t.Errorf("node id = %d, want %d", findings[0].NodeID, tc.wantNodeID)
			}
			if findings[0].Evidence["exclusive_pct"] == nil {
				t.Errorf("missing exclusive_pct evidence")
			}
		})
	}
}

func TestHotNode_SkipsNeverExecuted(t *testing.T) {
	p := loadRuleFixture(t, "hot_node", "positive.json")
	// Mark all nodes NeverExecuted; no findings should emerge.
	for _, n := range p.AllNodes() {
		n.NeverExecuted = true
	}
	if f := (&HotNode{}).Check(newContext(p)); len(f) != 0 {
		t.Errorf("NeverExecuted nodes should be skipped, got %d findings", len(f))
	}
}
