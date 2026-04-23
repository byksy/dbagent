package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/byksy/dbagent/internal/plan"
	"github.com/byksy/dbagent/internal/rules"
)

// summaryJSON is the JSON-friendly projection of a plan.Summary that
// also carries the findings list. Keeping it local keeps plan.Summary
// unaware of rules.
type summaryJSON struct {
	TotalTimeMs          float64         `json:"total_time_ms"`
	NodeCount            int             `json:"node_count"`
	SlowestNodeID        int             `json:"slowest_node_id,omitempty"`
	BiggestMisestimateID int             `json:"biggest_misestimate_id,omitempty"`
	WorstFilterRatioID   int             `json:"worst_filter_ratio_id,omitempty"`
	Findings             []rules.Finding `json:"findings"`
}

// jsonOutput is the wire shape written by --format json.
type jsonOutput struct {
	Plan    *plan.Plan  `json:"plan"`
	Summary summaryJSON `json:"summary"`
}

// renderJSON marshals the parsed plan, computed summary, and rule
// findings to JSON with two-space indent. The findings array is
// always present (possibly empty) so downstream tools can parse it
// without special-casing.
func renderJSON(w io.Writer, p *plan.Plan, s *plan.Summary, findings []rules.Finding) error {
	if findings == nil {
		findings = []rules.Finding{}
	}
	out := jsonOutput{
		Plan: p,
		Summary: summaryJSON{
			TotalTimeMs:          s.TotalTimeMs,
			NodeCount:            s.NodeCount,
			SlowestNodeID:        s.SlowestNodeID,
			BiggestMisestimateID: s.BiggestMisestimateID,
			WorstFilterRatioID:   s.WorstFilterRatioID,
			Findings:             findings,
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
