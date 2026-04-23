package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/byksy/dbagent/internal/plan"
	"github.com/byksy/dbagent/internal/rules"
)

// renderTable prints the plan as a flat aligned table, one row per
// node, with the node column indented by depth. Findings render as
// a trailing block after the summary.
func renderTable(w io.Writer, p *plan.Plan, s *plan.Summary, findings []rules.Finding) error {
	fmt.Fprintf(w, "Plan (total: %s, planning: %s, execution: %s)\n\n",
		formatDurationMs(p.TotalTimeMs),
		formatDurationMs(p.PlanningTimeMs),
		formatDurationMs(p.ExecutionTimeMs),
	)

	tw := tabwriter.NewWriter(w, 2, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "  #\ttime\texcl%\trows\tmisest\tloops\tnode")
	for _, n := range p.AllNodes() {
		writeTableRow(tw, n, p.TotalTimeMs)
	}
	_ = tw.Flush()

	fmt.Fprintln(w)
	writeSummary(w, p, s)
	if len(findings) > 0 {
		fmt.Fprintln(w)
		_ = formatFindingsSection(w, findings)
	}
	return nil
}

// writeTableRow renders a single node as a row. NeverExecuted nodes
// show a placeholder for numeric columns so the layout stays stable.
func writeTableRow(w io.Writer, n *plan.Node, totalTimeMs float64) {
	indent := ""
	if n.Depth > 0 {
		indent = strings.Repeat(" ", n.Depth) + "└ "
	}
	label := indent + n.NodeType.String()
	if n.NodeType == plan.NodeTypeUnknown && n.RawNodeType != "" {
		label = indent + n.RawNodeType
	}
	if n.RelationName != "" && n.Alias != "" && n.RelationName != n.Alias {
		label += " on " + n.RelationName + " " + n.Alias
	} else if n.RelationName != "" {
		label += " on " + n.RelationName
	}

	if n.NeverExecuted {
		fmt.Fprintf(w, "  %d\t—\t—\t—\t—\t—\t%s  (never executed)\n", n.ID, label)
		return
	}

	exclPct := "—"
	if totalTimeMs > 0 {
		exclPct = fmt.Sprintf("%.1f%%", n.ExclusiveTimeMs()/totalTimeMs*100)
	}

	fmt.Fprintf(w, "  %d\t%s\t%s\t%s\t%s\t%s\t%s\n",
		n.ID,
		formatDurationMs(n.ExclusiveTimeMs()),
		exclPct,
		formatInt(n.ActualRowsTotal()),
		formatMisest(n),
		formatInt(n.Loops),
		label,
	)
}

// formatMisest renders a MisestimateFactor with an arrow suffix.
// "1.0×" when the row count is equal, missing, or rounds to 1.0 —
// we don't want to show an arrow for factors that display as 1.0×,
// since the arrow implies a meaningful direction.
func formatMisest(n *plan.Node) string {
	f := n.MisestimateFactor()
	if f == 0 || f < 1.05 {
		return "1.0×"
	}
	suffix := ""
	switch n.MisestimateDirection() {
	case +1:
		suffix = " ↓"
	case -1:
		suffix = " ↑"
	}
	return fmt.Sprintf("%.1f×%s", f, suffix)
}
