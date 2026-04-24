package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/byksy/dbagent/internal/cli/style"
	"github.com/byksy/dbagent/internal/stats"
)

// renderStatsTerminal writes the workload snapshot to w using
// lipgloss-styled boxes, progress bars, and severity badges. Width
// is the total output width; 0 uses the current terminal width. The
// style package already handles the narrow-terminal fallback (no
// borders below 60 cols).
func renderStatsTerminal(w io.Writer, ws *stats.WorkloadStats, width int) error {
	if width <= 0 {
		width = terminalWidth()
	}
	if width > 120 {
		width = 120
	}

	sections := []func(io.Writer, *stats.WorkloadStats, int){
		writeStatsOverview,
		writeStatsTopTime,
		writeStatsTopCalls,
		writeStatsTopIO,
		writeStatsTopLowCache,
		writeStatsRecommendations,
	}
	for _, fn := range sections {
		fn(w, ws, width)
		fmt.Fprintln(w)
	}
	return nil
}

// writeStatsOverview renders the Database Overview box with the
// meta block and two progress bars (read/write split and cache hit).
func writeStatsOverview(w io.Writer, ws *stats.WorkloadStats, width int) {
	innerWidth := width - 4
	if innerWidth < 20 {
		innerWidth = 20
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Database:     %s (PostgreSQL %s)\n", ws.Meta.Database, ws.Meta.ServerVersion)
	fmt.Fprintf(&b, "Snapshot:     %s\n", ws.Meta.SnapshotAt.UTC().Format("2006-01-02 15:04:05 UTC"))
	if !ws.Meta.StatsSince.IsZero() {
		age := ws.Meta.SnapshotAt.Sub(ws.Meta.StatsSince)
		fmt.Fprintf(&b, "Stats since:  %s (%s ago)\n",
			ws.Meta.StatsSince.UTC().Format("2006-01-02 15:04:05"),
			formatCompactDuration(age))
	} else {
		fmt.Fprintf(&b, "Stats since:  unknown\n")
	}
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Total DB time:   %s\n", formatMsToHuman(ws.TotalTimeMs))
	fmt.Fprintf(&b, "Executions:      %s across %d unique queries\n",
		commaInt(ws.TotalExecutions), ws.TotalQueries)
	fmt.Fprintln(&b)

	// Cap the bar at ~40 cells so the label + percentage fit on one
	// line even when innerWidth is wide. Wider bars don't add info —
	// they're already showing a ratio.
	barWidth := innerWidth - 30
	if barWidth < 10 {
		barWidth = 10
	}
	if barWidth > 40 {
		barWidth = 40
	}

	if ws.TotalTimeMs > 0 {
		readShare := ws.ReadTimeMs / ws.TotalTimeMs
		writeShare := ws.WriteTimeMs / ws.TotalTimeMs
		otherShare := 1 - readShare - writeShare
		if otherShare < 0 {
			otherShare = 0
		}
		// Three-way text split so DDL / maintenance / transaction
		// control isn't invisible. Sum is always 100% (within
		// rounding), which the single-bar version couldn't honestly
		// claim — it showed "2% / 60%" even when the rest of the
		// workload was 38% other.
		fmt.Fprintf(&b, "Read/write:   reads %s · writes %s · other %s\n",
			style.StyleSuccess.Render(formatPctShort(readShare)),
			style.StyleInfo.Render(formatPctShort(writeShare)),
			style.StyleMuted.Render(formatPctShort(otherShare)))
	}
	if ws.CacheHitRatio >= 0 {
		fill := style.ColorBarFill
		if ws.CacheHitRatio < 0.7 {
			fill = style.ColorCritical
		} else if ws.CacheHitRatio < 0.9 {
			fill = style.ColorWarning
		}
		fmt.Fprintf(&b, "Cache hit:    %s\n",
			style.ProgressBar(ws.CacheHitRatio, barWidth, fill, style.ColorBarEmpty))
	}

	fmt.Fprint(w, style.Box("Database Overview", b.String(), width))
}

// writeStatsTopTime renders the "Top Queries by Total Time" box. We
// keep each section focused on one metric so the bars stay
// comparable row-to-row.
func writeStatsTopTime(w io.Writer, ws *stats.WorkloadStats, width int) {
	if len(ws.TopByTotalTime) == 0 {
		return
	}
	innerWidth := width - 4
	if innerWidth < 20 {
		innerWidth = 20
	}
	queryCol := innerWidth - 58
	if queryCol < 20 {
		queryCol = 20
	}

	var b strings.Builder
	fmt.Fprintln(&b, style.StyleMuted.Render(fmt.Sprintf("%-3s %-10s %-9s %-9s %-17s %s",
		"#", "calls", "mean", "total", "share", "query")))
	for _, q := range ws.TopByTotalTime {
		bar := style.ProgressBar(q.PctOfTotal, 16, style.ColorBarFill, style.ColorBarEmpty)
		fmt.Fprintf(&b, "%-3d %-10s %-9s %-9s %s  %s\n",
			q.Rank,
			commaInt(q.Calls),
			formatMsShort(q.MeanTimeMs),
			formatMsShort(q.TotalTimeMs),
			bar,
			truncateStatsText(q.QueryText, queryCol),
		)
	}
	fmt.Fprint(w, style.Box("Top Queries by Total Time", b.String(), width))
}

// writeStatsTopCalls complements Top Time with a call-count view —
// operators often ask "what's chatty?" vs "what's slow?".
func writeStatsTopCalls(w io.Writer, ws *stats.WorkloadStats, width int) {
	if len(ws.TopByCallCount) == 0 {
		return
	}
	innerWidth := width - 4
	queryCol := innerWidth - 40
	if queryCol < 20 {
		queryCol = 20
	}
	var b strings.Builder
	fmt.Fprintln(&b, style.StyleMuted.Render(fmt.Sprintf("%-3s %-12s %-9s %-9s  %s",
		"#", "calls", "mean", "total", "query")))
	for _, q := range ws.TopByCallCount {
		fmt.Fprintf(&b, "%-3d %-12s %-9s %-9s  %s\n",
			q.Rank,
			commaInt(q.Calls),
			formatMsShort(q.MeanTimeMs),
			formatMsShort(q.TotalTimeMs),
			truncateStatsText(q.QueryText, queryCol),
		)
	}
	fmt.Fprint(w, style.Box("Top Queries by Call Count", b.String(), width))
}

// writeStatsTopIO surfaces queries doing the most disk reads — the
// most impactful lens for buffer-pool tuning decisions.
func writeStatsTopIO(w io.Writer, ws *stats.WorkloadStats, width int) {
	if len(ws.TopByIO) == 0 {
		return
	}
	innerWidth := width - 4
	queryCol := innerWidth - 52
	if queryCol < 20 {
		queryCol = 20
	}
	var b strings.Builder
	fmt.Fprintln(&b, style.StyleMuted.Render(fmt.Sprintf("%-3s %-10s %-10s %-8s %-10s  %s",
		"#", "reads", "hits", "ratio", "total_time", "query")))
	for _, q := range ws.TopByIO {
		ratio := "—"
		if q.CacheHitRatio >= 0 {
			ratio = fmt.Sprintf("%.1f%%", q.CacheHitRatio*100)
		}
		fmt.Fprintf(&b, "%-3d %-10s %-10s %-8s %-10s  %s\n",
			q.Rank,
			commaInt(q.SharedBlksRead),
			commaInt(q.SharedBlksHit),
			ratio,
			formatMsShort(q.TotalTimeMs),
			truncateStatsText(q.QueryText, queryCol),
		)
	}
	fmt.Fprint(w, style.Box("Top Queries by I/O Reads", b.String(), width))
}

// writeStatsTopLowCache shows the worst per-query cache hit ratios.
// Rendered only when at least one entry qualifies — an all-healthy
// workload doesn't need to see an empty section.
func writeStatsTopLowCache(w io.Writer, ws *stats.WorkloadStats, width int) {
	if len(ws.TopByLowCache) == 0 {
		return
	}
	innerWidth := width - 4
	queryCol := innerWidth - 40
	if queryCol < 20 {
		queryCol = 20
	}
	var b strings.Builder
	fmt.Fprintln(&b, style.StyleMuted.Render(fmt.Sprintf("%-3s %-10s %-10s %-8s  %s",
		"#", "reads", "hits", "ratio", "query")))
	for _, q := range ws.TopByLowCache {
		ratio := "—"
		if q.CacheHitRatio >= 0 {
			ratio = fmt.Sprintf("%.1f%%", q.CacheHitRatio*100)
		}
		fmt.Fprintf(&b, "%-3d %-10s %-10s %-8s  %s\n",
			q.Rank,
			commaInt(q.SharedBlksRead),
			commaInt(q.SharedBlksHit),
			ratio,
			truncateStatsText(q.QueryText, queryCol),
		)
	}
	fmt.Fprint(w, style.Box("Top Queries by Low Cache Hit", b.String(), width))
}

// writeStatsRecommendations renders the recommendations section with
// severity icons, coloured labels, and optional action hints. Empty
// recommendation lists produce no output — empty boxes are clutter.
func writeStatsRecommendations(w io.Writer, ws *stats.WorkloadStats, width int) {
	if len(ws.Recommendations) == 0 {
		return
	}
	// Pre-wrap message / action text so continuation lines keep the
	// 13-column hanging indent. Box()'s automatic wrap would reset
	// continuation lines to column 0, losing the visual hierarchy.
	const indent = "             " // 13 spaces: icon + padding + [LABEL] pad
	wrapWidth := width - 4 - len(indent)
	if wrapWidth < 20 {
		wrapWidth = 20
	}

	var b strings.Builder
	for i, rec := range ws.Recommendations {
		if i > 0 {
			fmt.Fprintln(&b)
		}
		icon := style.SeverityStyle(rec.Severity).Render(style.SeverityIcon(rec.Severity))
		label := style.SeverityBadge(rec.Severity)
		fmt.Fprintf(&b, "%s  %s  %s\n", icon, label, style.StyleBold.Render(rec.Title))
		for _, line := range wrapHanging(rec.Message, wrapWidth) {
			fmt.Fprintf(&b, "%s%s\n", indent, line)
		}
		if rec.Action != "" {
			prefix := style.StyleMuted.Render("→") + " "
			actionLines := wrapHanging(rec.Action, wrapWidth-2)
			for j, line := range actionLines {
				if j == 0 {
					fmt.Fprintf(&b, "%s%s%s\n", indent, prefix, style.StyleMuted.Render(line))
				} else {
					fmt.Fprintf(&b, "%s  %s\n", indent, style.StyleMuted.Render(line))
				}
			}
		}
	}
	fmt.Fprint(w, style.Box("Recommendations", b.String(), width))
}

// wrapHanging breaks s at word boundaries so no line exceeds width
// visible columns. Words longer than width are allowed to overflow
// one line rather than splitting mid-word — legibility beats
// precision on narrow terminals.
func wrapHanging(s string, width int) []string {
	if width <= 0 || len(s) <= width {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	for _, word := range words {
		if cur.Len() == 0 {
			cur.WriteString(word)
			continue
		}
		if cur.Len()+1+len(word) > width {
			out = append(out, cur.String())
			cur.Reset()
			cur.WriteString(word)
			continue
		}
		cur.WriteByte(' ')
		cur.WriteString(word)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// truncateStatsText cuts q to width display runes, appending "…"
// when truncated. Whitespace normalisation happens upstream in
// normaliseWhitespace (stats.QueryGroup.QueryText), so this helper
// only needs to worry about length. Kept local so analyze remains
// untouched.
func truncateStatsText(q string, width int) string {
	if width <= 1 {
		return "…"
	}
	runes := []rune(q)
	if len(runes) <= width {
		return q
	}
	return string(runes[:width-1]) + "…"
}

// formatMsToHuman renders a millisecond total as "1h 23m" / "4m 12s"
// / "830ms" depending on scale. Used for the Overview's big numbers.
func formatMsToHuman(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	secs := int64(ms / 1000)
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	m := secs / 60
	s := secs % 60
	if m < 60 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	h := m / 60
	m = m % 60
	return fmt.Sprintf("%dh %02dm", h, m)
}

// formatMsShort is the per-row time cell format: "23.4ms", "2,925s"
// for anything above the 1s mark. Matches Stage 1's formatDuration
// layout but is a fresh implementation so we don't import pre-5.5
// code.
func formatMsShort(ms float64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%.1fms", ms)
	case ms < 10*1000:
		return fmt.Sprintf("%.1fs", ms/1000)
	case ms < 60*1000:
		return commaInt(int64(ms/1000+0.5)) + "s"
	}
	secs := int64(ms / 1000)
	m := secs / 60
	s := secs % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}

// formatPctShort renders a 0..1 ratio as "NN%".
func formatPctShort(ratio float64) string {
	return fmt.Sprintf("%d%%", int(ratio*100+0.5))
}

// formatCompactDuration renders a duration as "1d 5h" or "42m".
func formatCompactDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", h, m)
	}
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}

// commaInt inserts thousands separators. Kept local so the stats
// renderer doesn't reach into Stage 1 / Stage 2 code paths the
// 5.5 spec is explicitly fencing off.
func commaInt(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprint(n)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	first := len(s) % 3
	var out string
	if first > 0 {
		out = s[:first]
	}
	for i := first; i < len(s); i += 3 {
		if out != "" {
			out += ","
		}
		out += s[i : i+3]
	}
	if neg {
		return "-" + out
	}
	return out
}

