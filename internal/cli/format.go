package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// formatDuration renders a millisecond duration for human reading.
// Under 1000 ms it is shown with one decimal and a "ms" suffix.
// Above that it flips to seconds: below 100 s one decimal, at or above
// 100 s no decimals and comma-separated thousands. The threshold was
// chosen by eyeball — "142ms" reads clearly, but "1160423.5s" does not.
func formatDuration(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%.1fms", ms)
	}
	s := ms / 1000.0
	if s < 100 {
		return fmt.Sprintf("%.1fs", s)
	}
	return formatCount(int64(s+0.5)) + "s"
}

// formatCount renders an integer with comma thousand separators.
func formatCount(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	// Insert commas from the right.
	first := len(s) % 3
	var b strings.Builder
	if first > 0 {
		b.WriteString(s[:first])
	}
	for i := first; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// formatPercent returns "num/denom * 100" as a 1-decimal percentage,
// or "—" if denom is zero.
func formatPercent(num, denom float64) string {
	if denom == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", num/denom*100)
}

// formatCacheHitPct returns the buffer cache hit ratio as a percentage,
// or "—" if there were no buffer accesses at all.
func formatCacheHitPct(hit, read int64) string {
	total := hit + read
	if total == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", float64(hit)/float64(total)*100)
}

// truncateQuery collapses whitespace runs in q, trims it, and truncates
// to maxWidth runes with a "…" suffix. A maxWidth < 1 is treated as
// unbounded (no truncation).
func truncateQuery(q string, maxWidth int) string {
	cleaned := collapseWhitespace(q)
	if maxWidth < 1 {
		return cleaned
	}
	runes := []rune(cleaned)
	if len(runes) <= maxWidth {
		return cleaned
	}
	if maxWidth == 1 {
		return "…"
	}
	return string(runes[:maxWidth-1]) + "…"
}

// collapseWhitespace replaces any run of whitespace with a single space
// and trims surrounding space.
func collapseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r':
			if !inSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			inSpace = true
		default:
			b.WriteRune(r)
			inSpace = false
		}
	}
	out := b.String()
	return strings.TrimRight(out, " ")
}

// terminalWidth returns the current terminal width, or 120 if it
// cannot be determined (e.g., output is piped to a file).
func terminalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 120
	}
	return w
}
