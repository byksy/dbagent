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
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			p := loadPlanFixture(t, tc.fixture)
			var buf bytes.Buffer
			if err := renderTree(&buf, p, plan.Summarize(p), 100); err != nil {
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
			if err := renderTable(&buf, p, plan.Summarize(p), 100); err != nil {
				t.Fatalf("renderTable: %v", err)
			}
			checkGolden(t, filepath.Join(goldenDir, tc.golden), buf.String())
		})
	}
}

func TestRenderJSON_Structure(t *testing.T) {
	p := loadPlanFixture(t, "real/hash_join_with_filter.json")
	var buf bytes.Buffer
	if err := renderJSON(&buf, p, plan.Summarize(p)); err != nil {
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
	if err := renderTree(&buf, p, plan.Summarize(p), 100); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	if !strings.Contains(buf.String(), "(never executed)") {
		t.Errorf("expected '(never executed)' marker in output:\n%s", buf.String())
	}
}

func TestRenderTree_UnknownNodeType_NoPanic(t *testing.T) {
	p := loadPlanFixture(t, "synthetic/unknown_node_type.json")
	var buf bytes.Buffer
	if err := renderTree(&buf, p, plan.Summarize(p), 100); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	if !strings.Contains(buf.String(), "Imaginary Scan") {
		t.Errorf("expected raw node type 'Imaginary Scan' in output:\n%s", buf.String())
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
