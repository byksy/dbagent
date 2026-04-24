// Package style centralises dbagent's visual design tokens. Every
// other CLI rendering file in Stage 5.5+ imports from here — the
// existing pre-5.5 renderers (tree, table, findings) are left alone
// per the stage contract.
//
// When you need a new color, add it here rather than hard-coding an
// ANSI code elsewhere. When you need a new component, add it to
// components.go. The HTML exporter re-uses these tokens via
// CSSVariables() so terminal and HTML output stay in lockstep.
package style

import (
	"github.com/byksy/dbagent/internal/rules"
	"github.com/charmbracelet/lipgloss"
)

// AdaptiveColor chooses the Light or Dark value at runtime based on
// terminal background detection. Values are GitHub Primer's palette,
// picked for AA contrast against both backgrounds so findings stay
// legible regardless of the user's theme.
var (
	ColorCritical = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}
	ColorWarning  = lipgloss.AdaptiveColor{Light: "#BF8700", Dark: "#D29922"}
	ColorInfo     = lipgloss.AdaptiveColor{Light: "#0969DA", Dark: "#58A6FF"}
	ColorSuccess  = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"}
	ColorMuted    = lipgloss.AdaptiveColor{Light: "#57606A", Dark: "#8B949E"}
	ColorText     = lipgloss.AdaptiveColor{Light: "#1F2328", Dark: "#E6EDF3"}
	ColorBorder   = lipgloss.AdaptiveColor{Light: "#D0D7DE", Dark: "#30363D"}
	ColorBarFill  = lipgloss.AdaptiveColor{Light: "#0969DA", Dark: "#58A6FF"}
	ColorBarEmpty = lipgloss.AdaptiveColor{Light: "#D0D7DE", Dark: "#30363D"}
)

// Pre-built styles. Callers are free to copy and further-customise
// via lipgloss's builder API; lipgloss styles are immutable so no
// defensive copy is needed.
var (
	StyleCritical     = lipgloss.NewStyle().Foreground(ColorCritical).Bold(true)
	StyleWarning      = lipgloss.NewStyle().Foreground(ColorWarning).Bold(true)
	StyleInfo         = lipgloss.NewStyle().Foreground(ColorInfo)
	StyleSuccess      = lipgloss.NewStyle().Foreground(ColorSuccess)
	StyleMuted        = lipgloss.NewStyle().Foreground(ColorMuted)
	StyleBold         = lipgloss.NewStyle().Bold(true)
	StyleSectionTitle = lipgloss.NewStyle().Bold(true).Foreground(ColorText)
)

// Icons — unicode characters chosen for broad terminal support (macOS
// Terminal, iTerm2, xterm, Windows Terminal). Intentionally simple;
// emoji would pull in font-substitution issues.
const (
	IconCritical = "✗"
	IconWarning  = "⚠"
	IconInfo     = "ℹ"
	IconSuccess  = "✓"
	IconBullet   = "•"
)

// SeverityStyle maps a rules.Severity to its canonical lipgloss
// style. Unknown severities fall through to StyleMuted so they at
// least render something readable.
func SeverityStyle(s rules.Severity) lipgloss.Style {
	switch s {
	case rules.SeverityCritical:
		return StyleCritical
	case rules.SeverityWarning:
		return StyleWarning
	case rules.SeverityInfo:
		return StyleInfo
	}
	return StyleMuted
}

// SeverityIcon maps a rules.Severity to its unicode glyph.
func SeverityIcon(s rules.Severity) string {
	switch s {
	case rules.SeverityCritical:
		return IconCritical
	case rules.SeverityWarning:
		return IconWarning
	case rules.SeverityInfo:
		return IconInfo
	}
	return IconBullet
}

// SeverityLabel returns the upper-case severity name used in badges.
func SeverityLabel(s rules.Severity) string {
	switch s {
	case rules.SeverityCritical:
		return "CRITICAL"
	case rules.SeverityWarning:
		return "WARNING"
	case rules.SeverityInfo:
		return "INFO"
	}
	return "NOTE"
}

// CSSVariables returns the `:root` CSS block that mirrors the
// adaptive palette for the HTML exporter. Light mode is the default;
// the caller is expected to wrap a `@media (prefers-color-scheme:
// dark)` block around the dark values if it wants auto-switching.
// Keeping the generation here means terminal and HTML never drift
// apart.
func CSSVariables(dark bool) map[string]string {
	pick := func(c lipgloss.AdaptiveColor) string {
		if dark {
			return c.Dark
		}
		return c.Light
	}
	bg := "#ffffff"
	if dark {
		bg = "#0D1117"
	}
	return map[string]string{
		"--color-critical":  pick(ColorCritical),
		"--color-warning":   pick(ColorWarning),
		"--color-info":      pick(ColorInfo),
		"--color-success":   pick(ColorSuccess),
		"--color-muted":     pick(ColorMuted),
		"--color-text":      pick(ColorText),
		"--color-bg":        bg,
		"--color-border":    pick(ColorBorder),
		"--color-bar-fill":  pick(ColorBarFill),
		"--color-bar-empty": pick(ColorBarEmpty),
	}
}
