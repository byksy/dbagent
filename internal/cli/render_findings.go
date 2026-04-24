package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/byksy/dbagent/internal/plan"
	"github.com/byksy/dbagent/internal/rules"
)

// nodeIndex builds a NodeID → *plan.Node lookup so renderers can
// print a finding's target node with its full label.
func nodeIndex(p *plan.Plan) map[int]*plan.Node {
	if p == nil {
		return nil
	}
	idx := make(map[int]*plan.Node)
	for _, n := range p.AllNodes() {
		idx[n.ID] = n
	}
	return idx
}

// findingsByNode groups findings by their NodeID and returns both the
// map and a deterministically-ordered slice of node IDs, so renderers
// can iterate in a stable order.
func findingsByNode(findings []rules.Finding) (map[int][]rules.Finding, []int) {
	byID := map[int][]rules.Finding{}
	for _, f := range findings {
		byID[f.NodeID] = append(byID[f.NodeID], f)
	}
	ids := make([]int, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return byID, ids
}

// severityIcons returns an icon string summarising the highest
// severity present among the given findings. Only one icon per bucket;
// buckets are independent so Warning + Critical render as "⚠ ✗".
// Empty string if findings is empty.
func severityIcons(findings []rules.Finding) string {
	var hasInfo, hasWarn, hasCrit bool
	for _, f := range findings {
		switch f.Severity {
		case rules.SeverityCritical:
			hasCrit = true
		case rules.SeverityWarning:
			hasWarn = true
		case rules.SeverityInfo:
			hasInfo = true
		}
	}
	var parts []string
	if hasInfo {
		parts = append(parts, "ℹ")
	}
	if hasWarn {
		parts = append(parts, "⚠")
	}
	if hasCrit {
		parts = append(parts, "✗")
	}
	return strings.Join(parts, " ")
}

// countsBySeverity returns (critical, warning, info) counts. Handy
// for the trailing summary line "3 findings (1 critical, …)".
func countsBySeverity(findings []rules.Finding) (crit, warn, info int) {
	for _, f := range findings {
		switch f.Severity {
		case rules.SeverityCritical:
			crit++
		case rules.SeverityWarning:
			warn++
		case rules.SeverityInfo:
			info++
		}
	}
	return
}

// formatFindingsSection writes the Findings block to w, grouped by
// node with a trailing summary line. The plan argument lets each
// finding print its target node's full label ("[4] Seq Scan on orders");
// pass nil and findings will fall back to "[id]" alone.
// No output when findings is empty.
func formatFindingsSection(w io.Writer, p *plan.Plan, findings []rules.Finding) error {
	if len(findings) == 0 {
		return nil
	}
	fmt.Fprintln(w, "Findings")

	idx := nodeIndex(p)

	// Group findings by node; render nodes in Run()'s existing order
	// (severity-first), keyed by first-appearance NodeID so the block
	// reads top-down by urgency.
	seen := map[int]bool{}
	var nodeOrder []int
	for _, f := range findings {
		if !seen[f.NodeID] {
			seen[f.NodeID] = true
			nodeOrder = append(nodeOrder, f.NodeID)
		}
	}

	byNode := map[int][]rules.Finding{}
	for _, f := range findings {
		byNode[f.NodeID] = append(byNode[f.NodeID], f)
	}

	for _, id := range nodeOrder {
		writeNodeFindings(w, id, byNode[id], idx)
	}

	crit, warn, info := countsBySeverity(findings)
	fmt.Fprintf(w, "%d findings (%s)\n", len(findings), summaryCounts(crit, warn, info))
	return nil
}

// writeNodeFindings prints every finding attached to a node. Each
// finding gets its own severity tag so mixed-severity groups stay
// unambiguous. idx is used to render the node's full label.
func writeNodeFindings(w io.Writer, nodeID int, findings []rules.Finding, idx map[int]*plan.Node) {
	if len(findings) == 0 {
		return
	}
	target := nodeTarget(nodeID, idx)
	for _, f := range findings {
		tag := strings.ToUpper(f.Severity.String())
		fmt.Fprintf(w, "  %-8s  %s\n", tag, target)
		fmt.Fprintf(w, "            └─ %s\n", f.RuleID)
		for _, line := range wrapMessage(f.Message, 6) {
			fmt.Fprintf(w, "               %s\n", line)
		}
		if f.Suggested != "" {
			fmt.Fprintf(w, "               Suggested: %s\n", f.Suggested)
		}
		fmt.Fprintln(w)
	}
}

// nodeTarget renders the "where does this finding apply" label for
// the Findings section. For plan-level findings (NodeID == 0) we use
// "(plan-level)" so the text reads naturally and can't be confused
// with a node reference. For node findings we print the same
// "[ID] NodeType on relation" shape used elsewhere; if the plan
// lookup fails (e.g., legacy callers passed nil) we degrade to
// the bare "[ID]".
func nodeTarget(nodeID int, idx map[int]*plan.Node) string {
	if nodeID == 0 {
		return "(plan-level)"
	}
	if n, ok := idx[nodeID]; ok {
		return fmt.Sprintf("[%d] %s", nodeID, nodeLabel(n))
	}
	return fmt.Sprintf("[%d]", nodeID)
}

// wrapMessage currently returns a single-element slice; the indent
// argument is kept for future wrapping work. Keeping the signature
// stable lets us introduce wrapping later without a renderer change.
func wrapMessage(msg string, _ int) []string {
	return []string{msg}
}

// summaryCounts renders "1 critical, 2 warning, 3 info" skipping any
// zero-count bucket.
func summaryCounts(crit, warn, info int) string {
	var parts []string
	if crit > 0 {
		parts = append(parts, fmt.Sprintf("%d critical", crit))
	}
	if warn > 0 {
		parts = append(parts, fmt.Sprintf("%d warning", warn))
	}
	if info > 0 {
		parts = append(parts, fmt.Sprintf("%d info", info))
	}
	if len(parts) == 0 {
		return "0"
	}
	return strings.Join(parts, ", ")
}
