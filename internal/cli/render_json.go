package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/byksy/dbagent/internal/plan"
)

// jsonOutput is the wire shape written by --format json. Kept as a
// local type because Plan and Summary each already carry their own
// JSON tags suited for this view.
type jsonOutput struct {
	Plan    *plan.Plan    `json:"plan"`
	Summary *plan.Summary `json:"summary"`
}

// renderJSON marshals the parsed plan and computed summary to JSON
// with a two-space indent.
func renderJSON(w io.Writer, p *plan.Plan, s *plan.Summary) error {
	out := jsonOutput{Plan: p, Summary: s}
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
