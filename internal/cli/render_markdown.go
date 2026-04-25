package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/byksy/dbagent/internal/plan"
	"github.com/byksy/dbagent/internal/rules"
)

// renderMarkdown writes a GitHub-flavoured markdown report to w.
//
// Markdown is the "sharing" format — readers usually open these in a
// PR, an issue, or a chat thread, away from the original plan. Detail
// is therefore on by default: every finding gets its three-block
// writeup inside a <details> block, regardless of any --explain
// preference. The contract is "open this in any markdown renderer
// and have everything you need."
//
// The output is plain ASCII / unicode without ANSI escapes, since
// GitHub and most markdown renderers don't process them.
func renderMarkdown(w io.Writer, p *plan.Plan, summary *plan.Summary, findings []rules.Finding) error {
	writeMarkdownHeader(w, p)
	writeMarkdownPlan(w, p, findings)
	writeMarkdownSummary(w, p, summary)
	writeMarkdownFindings(w, p, findings)
	writeMarkdownFooter(w)
	return nil
}

// writeMarkdownHeader emits the H1 plus the bold meta lines that
// summarise the run at a glance.
func writeMarkdownHeader(w io.Writer, p *plan.Plan) {
	fmt.Fprintln(w, "# Query Analysis")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "**Total time:** %s (planning: %s, execution: %s)\n",
		formatDurationMs(p.TotalTimeMs),
		formatDurationMs(p.PlanningTimeMs),
		formatDurationMs(p.ExecutionTimeMs))
	fmt.Fprintf(w, "**Nodes:** %d\n", len(p.AllNodes()))
	if p.SourceDescription != "" {
		fmt.Fprintf(w, "**Source:** `%s`\n", p.SourceDescription)
	}
	fmt.Fprintln(w)
}

// writeMarkdownPlan writes the plan tree inside a fenced code block.
// The tree is generated via the same writeNode walker that powers
// `--format tree` so terminal and markdown stay in lockstep; the
// fence wrapper makes GitHub treat it as preformatted text rather
// than rendering each line as a paragraph.
func writeMarkdownPlan(w io.Writer, p *plan.Plan, findings []rules.Finding) {
	fmt.Fprintln(w, "## Plan")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "```")
	byNode, _ := findingsByNode(findings)
	writeNode(w, p.Root, "", true, byNode)
	fmt.Fprintln(w, "```")
	fmt.Fprintln(w)
}

// writeMarkdownSummary emits the Summary section as a bulleted list.
// Skips the section when the summary has nothing to say.
func writeMarkdownSummary(w io.Writer, p *plan.Plan, s *plan.Summary) {
	if s == nil || (s.SlowestNode == nil && s.BiggestMisestimate == nil && s.WorstFilterRatio == nil) {
		return
	}
	fmt.Fprintln(w, "## Summary")
	fmt.Fprintln(w)
	if n := s.SlowestNode; n != nil {
		pct := 0.0
		if p.TotalTimeMs > 0 {
			pct = n.ExclusiveTimeMs() / p.TotalTimeMs * 100
		}
		fmt.Fprintf(w, "- **Slowest node (exclusive):** [%d] %s — %s (%.0f%% of total)\n",
			n.ID, nodeLabel(n), formatDurationMs(n.ExclusiveTimeMs()), pct)
	}
	if n := s.BiggestMisestimate; n != nil {
		dir := "over"
		if n.MisestimateDirection() > 0 {
			dir = "under"
		}
		fmt.Fprintf(w, "- **Biggest row misestimate:** [%d] %s — planned %s, actual %s (%.1f× %s)\n",
			n.ID, nodeLabel(n),
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
		fmt.Fprintf(w, "- **Worst filter ratio:** [%d] %s — %.0f%% rows kept (%s removed)\n",
			n.ID, nodeLabel(n), keptPct, formatInt(n.RowsRemovedByFilter))
	}
	fmt.Fprintln(w)
}

// writeMarkdownFindings renders each finding as its own H3, with the
// rule's three-block explanation tucked inside a <details> block so
// the section stays scannable until the reader clicks through.
func writeMarkdownFindings(w io.Writer, p *plan.Plan, findings []rules.Finding) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintln(w, "## Findings")
	fmt.Fprintln(w)
	idx := nodeIndex(p)
	for _, f := range findings {
		writeMarkdownFinding(w, f, idx)
	}
	crit, warn, info := countsBySeverity(findings)
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "**Total:** %d findings (%s)\n",
		len(findings), summaryCounts(crit, warn, info))
	fmt.Fprintln(w)
}

// writeMarkdownFinding renders one finding. The H3 collapses the
// severity, target, and rule ID onto a single line so the table of
// contents in any markdown viewer stays useful.
func writeMarkdownFinding(w io.Writer, f rules.Finding, idx map[int]*plan.Node) {
	emoji := severityEmoji(f.Severity)
	sev := strings.ToUpper(f.SeverityName)
	target := nodeTarget(f.NodeID, idx)
	fmt.Fprintf(w, "### %s %s — %s · %s\n",
		emoji, sev, target, f.RuleID)
	fmt.Fprintln(w)
	fmt.Fprintln(w, f.Message)
	fmt.Fprintln(w)
	if f.Suggested != "" {
		fmt.Fprintln(w, "**Suggested:**")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "```sql")
		fmt.Fprintln(w, f.Suggested)
		fmt.Fprintln(w, "```")
		fmt.Fprintln(w)
	}
	if e := rules.LookupExplanation(f.RuleID); e != nil {
		fmt.Fprintln(w, "<details>")
		fmt.Fprintln(w, "<summary>Details</summary>")
		fmt.Fprintln(w)
		writeMarkdownExplanationBlock(w, "What happened", e.WhatHappened)
		writeMarkdownExplanationBlock(w, "Why it matters", e.WhyItMatters)
		writeMarkdownExplanationBlock(w, "What to do", e.WhatToDo)
		fmt.Fprintln(w, "</details>")
		fmt.Fprintln(w)
	}
}

// writeMarkdownExplanationBlock renders one labelled paragraph from
// the rule's explanation. The body is emitted verbatim — the YAML
// already preserves indentation for numbered lists and SQL snippets,
// and re-wrapping here would break them.
func writeMarkdownExplanationBlock(w io.Writer, label, body string) {
	fmt.Fprintf(w, "**%s**\n", label)
	fmt.Fprintln(w)
	fmt.Fprintln(w, strings.TrimRight(body, "\n"))
	fmt.Fprintln(w)
}

// writeMarkdownFooter prints the project pointer the audience needs
// when the report leaves its original context.
func writeMarkdownFooter(w io.Writer) {
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "*Generated by [dbagent](https://github.com/byksy/dbagent).*")
}

// severityEmoji maps a Severity to the emoji used at the start of
// each finding's H3. Reds, yellows, and blues render natively in
// every modern markdown client we care about.
func severityEmoji(s rules.Severity) string {
	switch s {
	case rules.SeverityCritical:
		return "🔴"
	case rules.SeverityWarning:
		return "🟡"
	case rules.SeverityInfo:
		return "🔵"
	}
	return ""
}
