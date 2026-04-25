package cli

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/byksy/dbagent/internal/plan"
	"github.com/byksy/dbagent/internal/rules"
)

// TestRenderMarkdown_Basic locks in the structural shape that
// downstream consumers (PR comments, Slack snippets) rely on:
// H1, fenced plan tree, sections, and footer all present in order.
func TestRenderMarkdown_Basic(t *testing.T) {
	p := loadPlanFixture(t, "real/simple_seq_scan.json")
	var buf bytes.Buffer
	if err := renderMarkdown(&buf, p, plan.Summarize(p), nil); err != nil {
		t.Fatalf("renderMarkdown: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"# Query Analysis",
		"## Plan",
		"```",
		"github.com/byksy/dbagent",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(out, "# Query Analysis") {
		t.Errorf("output must start with H1; got first 80 bytes: %q", out[:min(80, len(out))])
	}
}

// TestRenderMarkdown_WithFindings exercises the per-finding render
// path: severity emoji, target heading, message, Suggested SQL block,
// and the <details>-wrapped explanation.
func TestRenderMarkdown_WithFindings(t *testing.T) {
	p := loadPlanFixture(t, "rules/missing_index_on_filter/positive.json")
	findings := rules.Run(&rules.RuleContext{Plan: p}, rules.Default())
	if len(findings) == 0 {
		t.Fatalf("fixture should produce at least one finding")
	}
	var buf bytes.Buffer
	if err := renderMarkdown(&buf, p, plan.Summarize(p), findings); err != nil {
		t.Fatalf("renderMarkdown: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"## Findings",
		"<details>",
		"<summary>Details</summary>",
		"</details>",
		"**What happened**",
		"**Why it matters**",
		"**What to do**",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// Severity emoji shows up at least once for the rule we know fires.
	emojiHit := false
	for _, e := range []string{"🔴", "🟡", "🔵"} {
		if strings.Contains(out, e) {
			emojiHit = true
			break
		}
	}
	if !emojiHit {
		t.Errorf("expected at least one severity emoji in findings:\n%s", out)
	}
}

// TestRenderMarkdown_NoANSI guards the contract that markdown output
// is safe to paste into any GFM-compatible viewer. ANSI escapes
// would render as literal junk in GitHub, GitLab, and Slack.
func TestRenderMarkdown_NoANSI(t *testing.T) {
	p := loadPlanFixture(t, "rules/missing_index_on_filter/positive.json")
	findings := rules.Run(&rules.RuleContext{Plan: p}, rules.Default())
	var buf bytes.Buffer
	if err := renderMarkdown(&buf, p, plan.Summarize(p), findings); err != nil {
		t.Fatalf("renderMarkdown: %v", err)
	}
	ansi := regexp.MustCompile("\x1b\\[[0-9;]*m")
	if ansi.MatchString(buf.String()) {
		t.Errorf("markdown output contains ANSI escape sequences:\n%s", buf.String())
	}
}

// TestRenderMarkdown_Golden verifies the full byte-for-byte shape so
// drift in any helper is caught at review time. Regenerate with
// `go test ./internal/cli/ -run TestRenderMarkdown_Golden -update`.
func TestRenderMarkdown_Golden(t *testing.T) {
	cases := []struct {
		fixture string
		golden  string
	}{
		{"real/simple_seq_scan.json", "markdown_simple.md"},
		{"rules/missing_index_on_filter/positive.json", "markdown_with_findings.md"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			p := loadPlanFixture(t, tc.fixture)
			findings := rules.Run(&rules.RuleContext{Plan: p}, rules.Default())
			var buf bytes.Buffer
			if err := renderMarkdown(&buf, p, plan.Summarize(p), findings); err != nil {
				t.Fatalf("renderMarkdown: %v", err)
			}
			checkGolden(t, filepath.Join(goldenDir, tc.golden), buf.String())
		})
	}
}

