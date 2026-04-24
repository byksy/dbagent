package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/byksy/dbagent/internal/plan"
	"github.com/byksy/dbagent/internal/rules"
)

// findingJSON embeds rules.Finding and optionally attaches an
// Explanation. When --explain is not set, the field is nil and
// omitempty drops it from the wire representation entirely, so
// consumers that don't ask for explanations see exactly the same
// shape as before Stage 5.7.
type findingJSON struct {
	rules.Finding
	Explanation *rules.Explanation `json:"explanation,omitempty"`
}

// summaryJSON is the JSON-friendly projection of a plan.Summary that
// also carries the findings list. Keeping it local keeps plan.Summary
// unaware of rules.
type summaryJSON struct {
	TotalTimeMs          float64       `json:"total_time_ms"`
	NodeCount            int           `json:"node_count"`
	SlowestNodeID        int           `json:"slowest_node_id,omitempty"`
	BiggestMisestimateID int           `json:"biggest_misestimate_id,omitempty"`
	WorstFilterRatioID   int           `json:"worst_filter_ratio_id,omitempty"`
	Findings             []findingJSON `json:"findings"`
}

// jsonOutput is the wire shape written by --format json.
type jsonOutput struct {
	Plan    *plan.Plan  `json:"plan"`
	Summary summaryJSON `json:"summary"`
}

// renderJSON marshals the parsed plan, computed summary, and rule
// findings to JSON with two-space indent. The findings array is
// always present (possibly empty) so downstream tools can parse it
// without special-casing. Setting explain true attaches each
// finding's writeup under an "explanation" key.
func renderJSON(w io.Writer, p *plan.Plan, s *plan.Summary, findings []rules.Finding, explain bool) error {
	wrapped := make([]findingJSON, 0, len(findings))
	for _, f := range findings {
		row := findingJSON{Finding: f}
		if explain {
			row.Explanation = rules.LookupExplanation(f.RuleID)
		}
		wrapped = append(wrapped, row)
	}
	out := jsonOutput{
		Plan: p,
		Summary: summaryJSON{
			TotalTimeMs:          s.TotalTimeMs,
			NodeCount:            s.NodeCount,
			SlowestNodeID:        s.SlowestNodeID,
			BiggestMisestimateID: s.BiggestMisestimateID,
			WorstFilterRatioID:   s.WorstFilterRatioID,
			Findings:             wrapped,
		},
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("render json: %w", err)
	}
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("render json: %w", err)
	}
	if _, err := w.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("render json: %w", err)
	}
	return nil
}
