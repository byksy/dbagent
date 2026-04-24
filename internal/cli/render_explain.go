package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/byksy/dbagent/internal/cli/style"
	"github.com/byksy/dbagent/internal/rules"
)

// writeExplanation appends the three-block explanation for a finding
// underneath its one-line summary. The blocks are "What happened",
// "Why it matters", and "What to do"; each body is indented under
// the label so the structure is obvious even in plain-ASCII render.
//
// indent is the column-zero padding that lines up with the finding
// above. A blank line separates adjacent blocks; a missing rule ID
// (no entry in explanations.yaml) renders nothing rather than failing
// — callers that want strict coverage rely on the rules package's
// TestExplanations_AllRulesCovered to catch drift at build time.
func writeExplanation(w io.Writer, ruleID, indent string) {
	e := rules.LookupExplanation(ruleID)
	if e == nil {
		return
	}

	blocks := []struct {
		label string
		body  string
	}{
		{"What happened", e.WhatHappened},
		{"Why it matters", e.WhyItMatters},
		{"What to do", e.WhatToDo},
	}

	for i, b := range blocks {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s%s\n", indent, style.StyleBold.Render(b.label))
		for _, line := range strings.Split(strings.TrimRight(b.body, "\n"), "\n") {
			fmt.Fprintf(w, "%s  %s\n", indent, line)
		}
	}
}
