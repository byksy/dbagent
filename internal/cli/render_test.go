package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byksy/dbagent/internal/plan"
	"github.com/byksy/dbagent/internal/rules"
)

var update = flag.Bool("update", false, "update golden files")

const (
	realFixtureDir = "../../testdata/plans/real"
	goldenDir      = "../../testdata/golden"
)

func loadPlanFixture(t *testing.T, relPath string) *plan.Plan {
	t.Helper()
	path := filepath.Join("../../testdata/plans", relPath)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	p, err := plan.ParseBytes(b)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return p
}

// checkGolden compares got against the file at goldenPath, or rewrites
// the file when -update is set. Golden files are stored with LF line
// endings; this also normalises trailing whitespace.
func checkGolden(t *testing.T, goldenPath, got string) {
	t.Helper()
	got = normaliseGolden(got)

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create)", goldenPath, err)
	}
	if diff := firstDiff(string(want), got); diff != "" {
		t.Errorf("golden mismatch at %s:\n%s", goldenPath, diff)
	}
}

// normaliseGolden strips trailing spaces per line and enforces LF.
func normaliseGolden(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Join(lines, "\n")
}

// firstDiff returns a short description of the first differing line
// between want and got, or "" if they match.
func firstDiff(want, got string) string {
	if want == got {
		return ""
	}
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	n := len(wl)
	if len(gl) < n {
		n = len(gl)
	}
	for i := 0; i < n; i++ {
		if wl[i] != gl[i] {
			return "line " + itoa(i+1) + ":\n  want: " + quote(wl[i]) + "\n  got:  " + quote(gl[i])
		}
	}
	return "length differs: want " + itoa(len(wl)) + " lines, got " + itoa(len(gl)) + " lines"
}

func itoa(n int) string {
	return strings.TrimLeft(strings.Repeat(" ", 0)+""+formatIntMinimal(int64(n)), " ")
}

func formatIntMinimal(n int64) string {
	b := make([]byte, 0, 8)
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func quote(s string) string {
	return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
}

func TestRenderTree_Golden(t *testing.T) {
	cases := []struct {
		fixture string
		golden  string
	}{
		{"real/simple_seq_scan.json", "tree_simple_seq_scan.txt"},
		{"real/hash_join_with_filter.json", "tree_hash_join_with_filter.txt"},
		{"rules/missing_index_on_filter/positive.json", "tree_hash_join_with_findings.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			p := loadPlanFixture(t, tc.fixture)
			var buf bytes.Buffer
			if err := renderTree(&buf, p, plan.Summarize(p), rules.Run(&rules.RuleContext{Plan: p}, rules.Default()), false); err != nil {
				t.Fatalf("renderTree: %v", err)
			}
			checkGolden(t, filepath.Join(goldenDir, tc.golden), buf.String())
		})
	}
}

func TestRenderTable_Golden(t *testing.T) {
	cases := []struct {
		fixture string
		golden  string
	}{
		{"real/simple_seq_scan.json", "table_simple_seq_scan.txt"},
		{"real/hash_join_with_filter.json", "table_hash_join_with_filter.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			p := loadPlanFixture(t, tc.fixture)
			var buf bytes.Buffer
			if err := renderTable(&buf, p, plan.Summarize(p), rules.Run(&rules.RuleContext{Plan: p}, rules.Default()), false); err != nil {
				t.Fatalf("renderTable: %v", err)
			}
			checkGolden(t, filepath.Join(goldenDir, tc.golden), buf.String())
		})
	}
}

func TestRenderJSON_Structure(t *testing.T) {
	p := loadPlanFixture(t, "real/hash_join_with_filter.json")
	var buf bytes.Buffer
	if err := renderJSON(&buf, p, plan.Summarize(p), rules.Run(&rules.RuleContext{Plan: p}, rules.Default()), false); err != nil {
		t.Fatalf("renderJSON: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := out["plan"]; !ok {
		t.Errorf("missing top-level plan key")
	}
	s, ok := out["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary key missing or not an object")
	}
	nodeCountF, ok := s["node_count"].(float64)
	if !ok {
		t.Fatalf("summary.node_count missing or wrong type: %#v", s["node_count"])
	}
	actualCount := len(p.AllNodes())
	if int(nodeCountF) != actualCount {
		t.Errorf("summary.node_count = %d, want %d", int(nodeCountF), actualCount)
	}
}

func TestRenderTree_NeverExecuted_NoPanic(t *testing.T) {
	p := loadPlanFixture(t, "synthetic/never_executed_branch.json")
	var buf bytes.Buffer
	if err := renderTree(&buf, p, plan.Summarize(p), rules.Run(&rules.RuleContext{Plan: p}, rules.Default()), false); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	if !strings.Contains(buf.String(), "(never executed)") {
		t.Errorf("expected '(never executed)' marker in output:\n%s", buf.String())
	}
}

func TestRenderTree_UnknownNodeType_NoPanic(t *testing.T) {
	p := loadPlanFixture(t, "synthetic/unknown_node_type.json")
	var buf bytes.Buffer
	if err := renderTree(&buf, p, plan.Summarize(p), rules.Run(&rules.RuleContext{Plan: p}, rules.Default()), false); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	if !strings.Contains(buf.String(), "Imaginary Scan") {
		t.Errorf("expected raw node type 'Imaginary Scan' in output:\n%s", buf.String())
	}
}

// TestRenderTree_WithExplain_AddsBlocks verifies that --explain
// surfaces the three-block writeup below each finding's existing
// one-liner without disturbing the rest of the output.
func TestRenderTree_WithExplain_AddsBlocks(t *testing.T) {
	p := loadPlanFixture(t, "rules/missing_index_on_filter/positive.json")
	findings := rules.Run(&rules.RuleContext{Plan: p}, rules.Default())
	if len(findings) == 0 {
		t.Fatalf("fixture should produce at least one finding")
	}
	var buf bytes.Buffer
	if err := renderTree(&buf, p, plan.Summarize(p), findings, true); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	out := buf.String()
	for _, label := range []string{"What happened", "Why it matters", "What to do"} {
		if !strings.Contains(out, label) {
			t.Errorf("--explain output missing %q:\n%s", label, out)
		}
	}
}

// TestRenderTree_WithoutExplain_KeepsDefault confirms the plain
// renderer has no trace of Stage 5.7 labels — critical for keeping
// Stage 5.6 goldens stable and for the "pay only when you ask"
// principle.
func TestRenderTree_WithoutExplain_KeepsDefault(t *testing.T) {
	p := loadPlanFixture(t, "rules/missing_index_on_filter/positive.json")
	findings := rules.Run(&rules.RuleContext{Plan: p}, rules.Default())
	var buf bytes.Buffer
	if err := renderTree(&buf, p, plan.Summarize(p), findings, false); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	for _, forbidden := range []string{"What happened", "Why it matters", "What to do"} {
		if strings.Contains(buf.String(), forbidden) {
			t.Errorf("default tree output must not contain %q", forbidden)
		}
	}
}

// TestRenderTable_WithExplain_AddsBlocks mirrors the tree test for
// the table renderer: the table itself stays compact, but the
// Findings block gains the writeup when --explain is set.
func TestRenderTable_WithExplain_AddsBlocks(t *testing.T) {
	p := loadPlanFixture(t, "rules/missing_index_on_filter/positive.json")
	findings := rules.Run(&rules.RuleContext{Plan: p}, rules.Default())
	if len(findings) == 0 {
		t.Fatalf("fixture should produce at least one finding")
	}
	var buf bytes.Buffer
	if err := renderTable(&buf, p, plan.Summarize(p), findings, true); err != nil {
		t.Fatalf("renderTable: %v", err)
	}
	out := buf.String()
	for _, label := range []string{"What happened", "Why it matters", "What to do"} {
		if !strings.Contains(out, label) {
			t.Errorf("--explain output missing %q:\n%s", label, out)
		}
	}
}

// TestRenderJSON_WithExplain_IncludesBlocks ensures the wire shape
// gains an "explanation" object under each finding when --explain is
// set, with all three text blocks populated from the embedded YAML.
func TestRenderJSON_WithExplain_IncludesBlocks(t *testing.T) {
	p := loadPlanFixture(t, "rules/missing_index_on_filter/positive.json")
	findings := rules.Run(&rules.RuleContext{Plan: p}, rules.Default())
	if len(findings) == 0 {
		t.Fatalf("fixture should produce at least one finding")
	}
	var buf bytes.Buffer
	if err := renderJSON(&buf, p, plan.Summarize(p), findings, true); err != nil {
		t.Fatalf("renderJSON: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary missing or wrong type")
	}
	rawFindings, ok := summary["findings"].([]any)
	if !ok || len(rawFindings) == 0 {
		t.Fatalf("findings missing or empty: %#v", summary["findings"])
	}
	first, ok := rawFindings[0].(map[string]any)
	if !ok {
		t.Fatalf("first finding not an object: %#v", rawFindings[0])
	}
	explanation, ok := first["explanation"].(map[string]any)
	if !ok {
		t.Fatalf("first finding missing explanation: %#v", first)
	}
	for _, key := range []string{"what_happened", "why_it_matters", "what_to_do"} {
		s, ok := explanation[key].(string)
		if !ok || strings.TrimSpace(s) == "" {
			t.Errorf("explanation.%s missing or empty: %#v", key, explanation[key])
		}
	}
}

// TestRenderJSON_WithoutExplain_OmitsBlocks locks in the backward-
// compatible JSON shape: no "explanation" field when --explain is
// not set. Downstream consumers written against pre-5.7 output keep
// working as-is.
func TestRenderJSON_WithoutExplain_OmitsBlocks(t *testing.T) {
	p := loadPlanFixture(t, "rules/missing_index_on_filter/positive.json")
	findings := rules.Run(&rules.RuleContext{Plan: p}, rules.Default())
	var buf bytes.Buffer
	if err := renderJSON(&buf, p, plan.Summarize(p), findings, false); err != nil {
		t.Fatalf("renderJSON: %v", err)
	}
	if strings.Contains(buf.String(), `"explanation"`) {
		t.Errorf("default JSON output must not include explanation key:\n%s", buf.String())
	}
}

// realFixtureDir and goldenDir are used just to assert the directory
// exists at test time; catches the "forgot to run make fixtures" case.
func TestFixtureDirsExist(t *testing.T) {
	if _, err := os.Stat(realFixtureDir); err != nil {
		t.Fatalf("real fixture dir missing: %v — run scripts/capture-fixtures.sh", err)
	}
	if _, err := os.Stat(goldenDir); err != nil {
		t.Logf("golden dir %s does not yet exist; run with -update to create", goldenDir)
	}
}
