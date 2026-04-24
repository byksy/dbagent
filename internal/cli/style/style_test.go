package style

import (
	"os"
	"strings"
	"testing"

	"github.com/byksy/dbagent/internal/rules"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain forces lipgloss into ANSI-off mode for the whole package
// so palette and component tests don't depend on the runner's
// terminal capabilities. NO_COLOR propagation via env var wouldn't
// help: lipgloss reads the env once during init, so setting it per-
// test is racy.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}

func TestProgressBar(t *testing.T) {
	tests := []struct {
		name      string
		ratio     float64
		width     int
		wantHas   string
		wantNoBar bool
	}{
		{"half full at 20 chars", 0.5, 20, "50.0%", false},
		{"full at 20 chars", 1.0, 20, "100.0%", false},
		{"zero at 20 chars", 0.0, 20, "0.0%", false},
		{"clamps > 1", 1.5, 20, "100.0%", false},
		{"clamps < 0", -0.1, 20, "0.0%", false},
		{"tiny width drops bar", 0.5, 8, "50.0%", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := ProgressBar(tt.ratio, tt.width, ColorBarFill, ColorBarEmpty)
			if !strings.Contains(out, tt.wantHas) {
				t.Errorf("ProgressBar output missing %q: %q", tt.wantHas, out)
			}
			hasBar := strings.ContainsRune(out, '█') || strings.ContainsRune(out, '░')
			if tt.wantNoBar && hasBar {
				t.Errorf("expected no bar at tiny width, got %q", out)
			}
		})
	}
}

func TestBox_RendersBorder(t *testing.T) {
	out := Box("Test Title", "hello", 80)
	for _, want := range []string{"Test Title", "hello"} {
		if !strings.Contains(out, want) {
			t.Errorf("Box missing %q: %q", want, out)
		}
	}
	// Sharp corners + horizontal/vertical rules should appear since
	// width > minBoxWidth.
	for _, glyph := range []string{"┌", "┐", "└", "┘", "│", "─"} {
		if !strings.Contains(out, glyph) {
			t.Errorf("Box expected border glyph %q, got %q", glyph, out)
		}
	}
	// Title must sit inside the top border, not on an interior line.
	lines := strings.Split(out, "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], "Test Title") {
		t.Errorf("title should be embedded in top border, got first line %q", lines[0])
	}
}

func TestBox_CompactFallback(t *testing.T) {
	out := Box("Narrow", "content", 40)
	if strings.ContainsAny(out, "┌└┐┘│") {
		t.Errorf("narrow Box should skip borders, got %q", out)
	}
	if !strings.Contains(out, "Narrow") || !strings.Contains(out, "content") {
		t.Errorf("narrow Box should still show title + content: %q", out)
	}
}

func TestSeverityMapping(t *testing.T) {
	tests := []struct {
		sev       rules.Severity
		wantLabel string
		wantIcon  string
	}{
		{rules.SeverityCritical, "CRITICAL", IconCritical},
		{rules.SeverityWarning, "WARNING", IconWarning},
		{rules.SeverityInfo, "INFO", IconInfo},
	}
	for _, tt := range tests {
		t.Run(tt.wantLabel, func(t *testing.T) {
			if got := SeverityLabel(tt.sev); got != tt.wantLabel {
				t.Errorf("label = %q, want %q", got, tt.wantLabel)
			}
			if got := SeverityIcon(tt.sev); got != tt.wantIcon {
				t.Errorf("icon = %q, want %q", got, tt.wantIcon)
			}
		})
	}
}

func TestSeverityBadge_ContainsLabel(t *testing.T) {
	b := SeverityBadge(rules.SeverityCritical)
	if !strings.Contains(b, "[CRITICAL]") {
		t.Errorf("badge missing label: %q", b)
	}
}

func TestTableRow_TruncatesOverflow(t *testing.T) {
	out := TableRow([]Column{
		{Content: "short", Width: 10, Align: AlignLeft},
		{Content: "this-is-way-too-long-for-its-column", Width: 8, Align: AlignLeft},
	})
	if !strings.Contains(out, "…") {
		t.Errorf("expected ellipsis truncation, got %q", out)
	}
}

func TestCSSVariables_LightDarkDivergent(t *testing.T) {
	light := CSSVariables(false)
	dark := CSSVariables(true)
	if light["--color-text"] == dark["--color-text"] {
		t.Errorf("light and dark text colours should differ")
	}
	for _, k := range []string{"--color-bg", "--color-critical", "--color-info"} {
		if _, ok := light[k]; !ok {
			t.Errorf("light CSS missing %s", k)
		}
	}
}
