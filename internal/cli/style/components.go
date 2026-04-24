package style

import (
	"fmt"
	"strings"

	"github.com/byksy/dbagent/internal/rules"
	"github.com/charmbracelet/lipgloss"
)

// Alignment controls horizontal alignment inside a TableRow column.
type Alignment int

const (
	AlignLeft Alignment = iota
	AlignRight
)

// Default box width, used when the caller passes 0. 80 columns has
// been the standard "works everywhere" terminal width since VT100.
const defaultBoxWidth = 80

// minBoxWidth is the narrow-terminal fallback threshold. Below this,
// Box() degrades to bare content separated by blank lines — borders
// on a 40-column terminal produce cramped output that's worse than
// no borders.
const minBoxWidth = 60

// Box wraps content in a titled, bordered rectangle with the title
// embedded in the top border ("┌─ Title ──────┐"). The width
// argument is the total outer width including borders; 0 picks the
// default. Content is rendered as-is, so callers should pre-wrap it
// to width-4 (two border columns + two padding columns).
//
// Below minBoxWidth, Box skips the border and returns a plain
// title-then-content layout. This keeps output legible on narrow
// terminals (mobile SSH, split panes) without introducing a second
// code path for callers.
func Box(title, content string, width int) string {
	if width <= 0 {
		width = defaultBoxWidth
	}
	if width < minBoxWidth {
		return compactBox(title, content)
	}

	innerWidth := width - 2 // space between the two vertical border chars

	borderStyle := lipgloss.NewStyle().Foreground(ColorBorder)
	titleStyle := StyleSectionTitle

	// Top border: "┌─ Title ───...───┐". Reserve a minimum of two
	// trailing dashes so the label never butts against the corner.
	const titlePrefix = "─ "
	const titleSuffix = " "
	titleVisual := len([]rune(titlePrefix)) + len([]rune(title)) + len([]rune(titleSuffix))
	dashCount := innerWidth - titleVisual
	if dashCount < 2 {
		dashCount = 2
	}
	top := borderStyle.Render("┌"+titlePrefix) +
		titleStyle.Render(title) +
		borderStyle.Render(titleSuffix+strings.Repeat("─", dashCount)+"┐")

	// Body lines: "│ {line padded to innerWidth-2} │" — the -2 leaves
	// one padding column on each side inside the verticals.
	contentInner := innerWidth - 2
	if contentInner < 1 {
		contentInner = 1
	}
	leftWall := borderStyle.Render("│") + " "
	rightWall := " " + borderStyle.Render("│")

	// Wrap each input line to contentInner so callers that emit long
	// prose (recommendations, action hints) don't push the right wall
	// past the terminal edge. lipgloss.Width() is ANSI-aware, so
	// styled spans are measured correctly; wrapping is done on word
	// boundaries where possible.
	var body strings.Builder
	wrapStyle := lipgloss.NewStyle().Width(contentInner)
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	for _, line := range lines {
		wrapped := wrapStyle.Render(line)
		for _, sub := range strings.Split(wrapped, "\n") {
			visible := lipgloss.Width(sub)
			pad := contentInner - visible
			if pad < 0 {
				pad = 0
			}
			body.WriteString(leftWall)
			body.WriteString(sub)
			body.WriteString(strings.Repeat(" ", pad))
			body.WriteString(rightWall)
			body.WriteString("\n")
		}
	}

	bottom := borderStyle.Render("└" + strings.Repeat("─", innerWidth) + "┘")

	return top + "\n" + body.String() + bottom + "\n"
}

// compactBox produces a border-free alternative for narrow terminals.
// Keeps the same section title / content shape so callers don't need
// to branch.
func compactBox(title, content string) string {
	var b strings.Builder
	b.WriteString(StyleSectionTitle.Render(title))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", len([]rune(title))))
	b.WriteString("\n")
	b.WriteString(content)
	return b.String()
}

// ProgressBar renders a horizontal bar + trailing percentage. ratio
// is clamped to [0, 1]. width is the total rendered width including
// the percentage; bars shorter than 10 characters degrade to a plain
// percentage since a 3-cell bar is useless.
//
// The bar colours itself with filledColor / emptyColor; callers
// pass palette values so dark/light mode adapts automatically.
func ProgressBar(ratio float64, width int, filledColor, emptyColor lipgloss.AdaptiveColor) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	pct := fmt.Sprintf(" %5.1f%%", ratio*100)
	if width < 10 {
		return strings.TrimSpace(pct)
	}
	barWidth := width - len(pct)
	if barWidth < 4 {
		return strings.TrimSpace(pct)
	}
	filled := int(ratio*float64(barWidth) + 0.5)
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled
	fill := lipgloss.NewStyle().Foreground(filledColor).Render(strings.Repeat("█", filled))
	rest := lipgloss.NewStyle().Foreground(emptyColor).Render(strings.Repeat("░", empty))
	return fill + rest + pct
}

// SeverityBadge renders "[SEVERITY]" with the corresponding colour.
// Callers precede it with an icon if they want both, since icons
// and badges are used together in the recommendations section but
// separately elsewhere.
func SeverityBadge(s rules.Severity) string {
	return SeverityStyle(s).Render("[" + SeverityLabel(s) + "]")
}

// SectionTitle returns a consistently-formatted section heading.
// Renders as "┌─ Title ──────────────┐" up to width. Used by pre-
// box rendering where Box() isn't appropriate (e.g., sub-sections
// inside a larger box).
func SectionTitle(title string, width int) string {
	if width <= 0 {
		width = defaultBoxWidth
	}
	prefix := "┌─ "
	suffix := " ─┐"
	inner := " " + title + " "
	pad := width - len([]rune(prefix)) - len([]rune(inner)) - len([]rune(suffix))
	if pad < 0 {
		pad = 0
	}
	return StyleSectionTitle.Render(prefix+title+" ") +
		lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", pad)+suffix)
}

// Column describes one cell in a TableRow.
type Column struct {
	Content string
	Width   int
	Align   Alignment
	Style   lipgloss.Style
}

// TableRow formats a single row with fixed-width columns. Width is
// the column's outer cell width (lipgloss pads to it). Content wider
// than Width is truncated with "…" as a suffix so rows stay aligned
// across wide tables.
func TableRow(cols []Column) string {
	parts := make([]string, 0, len(cols))
	for _, c := range cols {
		content := c.Content
		if c.Width > 0 && lipgloss.Width(content) > c.Width {
			content = truncate(content, c.Width)
		}
		cell := c.Style.Copy()
		if c.Width > 0 {
			cell = cell.Width(c.Width)
		}
		if c.Align == AlignRight {
			cell = cell.Align(lipgloss.Right)
		}
		parts = append(parts, cell.Render(content))
	}
	return strings.Join(parts, " ")
}

// truncate shortens s to at most width display columns, appending
// "…" to signal truncation. Runes, not bytes — multi-byte characters
// count as one column.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}
