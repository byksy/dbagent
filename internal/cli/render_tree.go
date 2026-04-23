package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/byksy/dbagent/internal/plan"
)

// renderTree renders a Plan as an indented box-drawing tree followed
// by a summary block.
func renderTree(w io.Writer, p *plan.Plan, s *plan.Summary) error {
	fmt.Fprintf(w, "Plan (total: %s, planning: %s, execution: %s)\n\n",
		formatDurationMs(p.TotalTimeMs),
		formatDurationMs(p.PlanningTimeMs),
		formatDurationMs(p.ExecutionTimeMs),
	)
	writeNode(w, p.Root, "", true)
	fmt.Fprintln(w)
	writeSummary(w, p, s)
	return nil
}

// writeNode writes one node and recurses into its children. "prefix"
// is the continuation characters accumulated from ancestors; "isLast"
// tells us which junction character to pick for this node.
func writeNode(w io.Writer, n *plan.Node, prefix string, isLast bool) {
	if n == nil {
		return
	}

	branch := "├─ "
	childPrefix := prefix + "│   "
	if isLast {
		branch = "└─ "
		childPrefix = prefix + "    "
	}

	// Root has no branch character; everything else does.
	headerPrefix := prefix
	if n.Depth > 0 {
		headerPrefix = prefix + branch
	}

	fmt.Fprintln(w, headerPrefix+nodeHeader(n))

	detailPrefix := childPrefix
	if n.Depth == 0 {
		detailPrefix = prefix + "    "
	}

	for _, line := range nodeDetails(n) {
		fmt.Fprintln(w, strings.TrimRight(detailPrefix+line, " "))
	}

	for i, c := range n.Children {
		last := i == len(n.Children)-1
		nextPrefix := prefix + "│   "
		if n.Depth == 0 {
			nextPrefix = ""
		} else if isLast {
			nextPrefix = prefix + "    "
		}
		writeNode(w, c, nextPrefix, last)
	}
}

// nodeHeader returns the one-line summary for a node.
func nodeHeader(n *plan.Node) string {
	name := n.NodeType.String()
	if n.NodeType == plan.NodeTypeUnknown && n.RawNodeType != "" {
		name = n.RawNodeType
	}
	target := ""
	switch {
	case n.RelationName != "" && n.Alias != "" && n.RelationName != n.Alias:
		target = fmt.Sprintf(" on %s %s", n.RelationName, n.Alias)
	case n.RelationName != "":
		target = fmt.Sprintf(" on %s", n.RelationName)
	case n.Alias != "" && n.NodeType == plan.NodeTypeCTEScan:
		target = fmt.Sprintf(" on %s", n.Alias)
	}

	if n.NeverExecuted {
		return fmt.Sprintf("[%d] %s%s  (never executed)", n.ID, name, target)
	}
	return fmt.Sprintf("[%d] %s%s  rows=%s  time=%s  loops=%s",
		n.ID, name, target,
		formatInt(n.ActualRowsTotal()),
		formatDurationMs(n.InclusiveTimeMs()),
		formatInt(n.Loops),
	)
}

// nodeDetails returns the zero-or-more detail lines that follow the
// header. Lines are elided when the corresponding field is empty or
// the node was never executed.
func nodeDetails(n *plan.Node) []string {
	if n.NeverExecuted {
		return nil
	}
	var lines []string

	if n.IndexName != "" || n.IndexCond != "" {
		parts := []string{}
		if n.IndexName != "" {
			parts = append(parts, "index="+n.IndexName)
		}
		if n.IndexCond != "" {
			parts = append(parts, "index_cond="+n.IndexCond)
		}
		if n.RecheckCond != "" {
			parts = append(parts, "recheck="+n.RecheckCond)
		}
		lines = append(lines, strings.Join(parts, "  "))
	}

	if n.HashCond != "" {
		lines = append(lines, "hash_cond="+n.HashCond)
	}
	if n.MergeCond != "" {
		lines = append(lines, "merge_cond="+n.MergeCond)
	}
	if n.JoinFilter != "" {
		parts := []string{"join_filter=" + n.JoinFilter}
		if n.RowsRemovedByJoinFilter > 0 {
			parts = append(parts, fmt.Sprintf("join_removed=%s", formatInt(n.RowsRemovedByJoinFilter)))
		}
		lines = append(lines, strings.Join(parts, "  "))
	}

	if n.Filter != "" {
		parts := []string{"filter=" + n.Filter}
		if n.RowsRemovedByFilter > 0 {
			parts = append(parts, fmt.Sprintf("filter_removed=%s", formatInt(n.RowsRemovedByFilter)))
		}
		lines = append(lines, strings.Join(parts, "  "))
	}

	if len(n.SortKey) > 0 {
		parts := []string{"sort_key=" + strings.Join(n.SortKey, ", ")}
		if n.SortMethod != "" {
			parts = append(parts, "method="+n.SortMethod)
		}
		if n.SortSpaceKB > 0 {
			parts = append(parts, fmt.Sprintf("memory=%dkB", n.SortSpaceKB))
		}
		lines = append(lines, strings.Join(parts, "  "))
	}

	if len(n.GroupKey) > 0 {
		parts := []string{"group_key=" + strings.Join(n.GroupKey, ", ")}
		if n.Strategy != "" {
			parts = append(parts, "strategy="+n.Strategy)
		}
		lines = append(lines, strings.Join(parts, "  "))
	}

	if n.WorkersLaunched > 0 {
		lines = append(lines, fmt.Sprintf("workers: planned=%d launched=%d", n.WorkersPlanned, n.WorkersLaunched))
	}

	if n.SharedHitBlocks > 0 || n.SharedReadBlocks > 0 || n.SharedDirtiedBlocks > 0 || n.SharedWrittenBlocks > 0 {
		parts := []string{fmt.Sprintf("shared hit=%s read=%s", formatInt(n.SharedHitBlocks), formatInt(n.SharedReadBlocks))}
		if n.SharedDirtiedBlocks > 0 {
			parts = append(parts, fmt.Sprintf("dirtied=%s", formatInt(n.SharedDirtiedBlocks)))
		}
		if n.SharedWrittenBlocks > 0 {
			parts = append(parts, fmt.Sprintf("written=%s", formatInt(n.SharedWrittenBlocks)))
		}
		lines = append(lines, "buffers: "+strings.Join(parts, " "))
	}

	return lines
}

// writeSummary prints the trailing summary block. If nothing qualifies,
// the whole block is omitted.
func writeSummary(w io.Writer, p *plan.Plan, s *plan.Summary) {
	if s == nil || (s.SlowestNode == nil && s.BiggestMisestimate == nil && s.WorstFilterRatio == nil) {
		return
	}
	fmt.Fprintln(w, "Summary")
	if n := s.SlowestNode; n != nil {
		pct := 0.0
		if p.TotalTimeMs > 0 {
			pct = n.ExclusiveTimeMs() / p.TotalTimeMs * 100
		}
		fmt.Fprintf(w, "  Slowest node (exclusive):  [%d] %s — %s (%.0f%% of total)\n",
			n.ID, nodeShortName(n), formatDurationMs(n.ExclusiveTimeMs()), pct)
	}
	if n := s.BiggestMisestimate; n != nil {
		dir := "over"
		if n.MisestimateDirection() > 0 {
			dir = "under"
		}
		fmt.Fprintf(w, "  Biggest row misestimate:   [%d] %s — planned %s, actual %s (%.1f× %s)\n",
			n.ID, nodeShortName(n),
			formatInt(n.PlanRows), formatInt(n.ActualRowsTotal()),
			n.MisestimateFactor(), dir)
	}
	if n := s.WorstFilterRatio; n != nil {
		kept := n.ActualRows * max64(n.Loops, 1)
		total := kept + n.RowsRemovedByFilter
		keptPct := 0.0
		if total > 0 {
			keptPct = float64(kept) / float64(total) * 100
		}
		fmt.Fprintf(w, "  Worst filter ratio:        [%d] %s — %.0f%% rows kept (%s removed)\n",
			n.ID, nodeShortName(n), keptPct, formatInt(n.RowsRemovedByFilter))
	}
}

// nodeShortName is "NodeType on relation" without IDs or rows — used
// only in the summary block.
func nodeShortName(n *plan.Node) string {
	name := n.NodeType.String()
	if n.NodeType == plan.NodeTypeUnknown && n.RawNodeType != "" {
		name = n.RawNodeType
	}
	if n.RelationName != "" && n.Alias != "" && n.RelationName != n.Alias {
		return fmt.Sprintf("%s on %s %s", name, n.RelationName, n.Alias)
	}
	if n.RelationName != "" {
		return fmt.Sprintf("%s on %s", name, n.RelationName)
	}
	return name
}

// formatDurationMs formats a millisecond value with readable units.
// Below 1s: 1-decimal ms. Below 10s: 1-decimal seconds. Below 60s:
// whole seconds with thousands separator. Otherwise "Xm Y.Ys".
func formatDurationMs(ms float64) string {
	switch {
	case ms < 0:
		return formatDurationMs(-ms)
	case ms < 1000:
		return fmt.Sprintf("%.1fms", ms)
	case ms < 10000:
		return fmt.Sprintf("%.1fs", ms/1000)
	case ms < 60000:
		return formatInt(int64(ms/1000+0.5)) + "s"
	default:
		secs := int64(ms / 1000)
		m := secs / 60
		s := float64(secs%60) + (ms-float64(secs*1000))/1000
		return fmt.Sprintf("%dm%.1fs", m, s)
	}
}

// formatInt inserts comma thousands separators. Duplicates logic from
// Stage 1's formatCount on purpose — the two live in different files
// and we don't want Stage 2 pulling Stage 1's package-private helper
// through an unrelated rename. Small, pure, harmless.
func formatInt(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
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

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
