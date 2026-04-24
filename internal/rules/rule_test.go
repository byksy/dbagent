package rules

import (
	"sort"
	"testing"
)

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		in   Severity
		want string
	}{
		{SeverityInfo, "info"},
		{SeverityWarning, "warning"},
		{SeverityCritical, "critical"},
	}
	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		in      string
		want    Severity
		wantErr bool
	}{
		{"info", SeverityInfo, false},
		{"INFO", SeverityInfo, false},
		{" warning ", SeverityWarning, false},
		{"warn", SeverityWarning, false},
		{"critical", SeverityCritical, false},
		{"crit", SeverityCritical, false},
		{"", 0, true},
		{"fatal", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseSeverity(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseSeverity(%q): expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSeverity(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("ParseSeverity(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestCategory_String(t *testing.T) {
	if CategoryDiagnostic.String() != "diagnostic" {
		t.Errorf("CategoryDiagnostic.String() mismatch")
	}
	if CategoryPrescriptive.String() != "prescriptive" {
		t.Errorf("CategoryPrescriptive.String() mismatch")
	}
}

func TestDefault_AllRulesPresent(t *testing.T) {
	want := []string{
		"bitmap_and_composite",
		"composite_index_extension",
		"cte_cartesian_product",
		"duplicate_index",
		"filter_removal_ratio",
		"fk_missing_index",
		"hot_node",
		"memoize_opportunity",
		"missing_index_on_filter",
		"network_overhead",
		"planning_vs_execution",
		"redundant_aggregation",
		"row_misestimate",
		"sort_spilled",
		"table_bloat",
		"unused_index_hint",
		"worker_shortage",
	}
	got := make([]string, 0, len(Default()))
	for _, r := range Default() {
		got = append(got, r.ID())
	}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("Default() returned %d rules, want %d: got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rule[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDefault_RuleCountAt17(t *testing.T) {
	if got := len(Default()); got != 17 {
		t.Errorf("Default() rule count = %d, want 17", got)
	}
}

func TestDefault_NoDuplicateIDs(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Default() {
		if seen[r.ID()] {
			t.Fatalf("duplicate rule id: %s", r.ID())
		}
		seen[r.ID()] = true
	}
}

// fakeRule is a test-only Rule that emits a fixed set of findings.
type fakeRule struct {
	id       string
	name     string
	findings []Finding
}

func (f *fakeRule) ID() string                    { return f.id }
func (f *fakeRule) Name() string                  { return f.name }
func (f *fakeRule) Category() Category            { return CategoryDiagnostic }
func (f *fakeRule) Check(_ *RuleContext) []Finding { return f.findings }

func TestRun_Ordering(t *testing.T) {
	// Craft three rules that emit findings at different severities and
	// node IDs; assert deterministic order (sev desc, NodeID asc,
	// RuleID asc).
	a := &fakeRule{id: "a_rule", name: "A", findings: []Finding{
		{RuleID: "a_rule", Severity: SeverityWarning, NodeID: 2},
		{RuleID: "a_rule", Severity: SeverityCritical, NodeID: 5},
	}}
	b := &fakeRule{id: "b_rule", name: "B", findings: []Finding{
		{RuleID: "b_rule", Severity: SeverityCritical, NodeID: 3},
		{RuleID: "b_rule", Severity: SeverityInfo, NodeID: 1},
	}}
	c := &fakeRule{id: "c_rule", name: "C", findings: []Finding{
		{RuleID: "c_rule", Severity: SeverityWarning, NodeID: 2},
	}}
	out := Run(&RuleContext{}, []Rule{a, b, c})
	want := []struct {
		sev    Severity
		nodeID int
		ruleID string
	}{
		{SeverityCritical, 3, "b_rule"},
		{SeverityCritical, 5, "a_rule"},
		{SeverityWarning, 2, "a_rule"},
		{SeverityWarning, 2, "c_rule"},
		{SeverityInfo, 1, "b_rule"},
	}
	if len(out) != len(want) {
		t.Fatalf("Run: got %d findings, want %d", len(out), len(want))
	}
	for i, w := range want {
		if out[i].Severity != w.sev || out[i].NodeID != w.nodeID || out[i].RuleID != w.ruleID {
			t.Errorf("position %d: got {sev=%v node=%d id=%s}, want {sev=%v node=%d id=%s}",
				i, out[i].Severity, out[i].NodeID, out[i].RuleID, w.sev, w.nodeID, w.ruleID)
		}
	}
}
