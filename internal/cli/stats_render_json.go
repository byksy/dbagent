package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/byksy/dbagent/internal/stats"
)

// renderStatsJSON writes a WorkloadStats as pretty-printed JSON. The
// output is expected to be pipeline-safe (no ANSI, stable field
// ordering) and to validate against schemas/stats-v1.json. An
// additional top-level "$schema" field points consumers at the
// schema for programmatic validation.
func renderStatsJSON(w io.Writer, ws *stats.WorkloadStats) error {
	// Wrap the WorkloadStats in an envelope so we can add $schema
	// without polluting the main struct's JSON tags.
	envelope := struct {
		Schema string               `json:"$schema"`
		Data   *stats.WorkloadStats `json:",inline"`
	}{
		Schema: "schemas/stats-v1.json",
	}
	// Encoding our `,inline` tag by hand: marshal the Data, then
	// merge its top-level keys with the Schema tag. encoding/json
	// doesn't support ,inline natively so we splice at the byte
	// level.
	dataBytes, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return fmt.Errorf("render json: marshal stats: %w", err)
	}
	// Strip the surrounding `{`/`}` so the inner fields can join
	// the envelope's own key.
	if len(dataBytes) < 2 || dataBytes[0] != '{' || dataBytes[len(dataBytes)-1] != '}' {
		return fmt.Errorf("render json: unexpected stats shape")
	}
	inner := dataBytes[1 : len(dataBytes)-1]

	schemaLine, _ := json.Marshal(envelope.Schema)
	out := append([]byte(`{`), []byte("\n  \"$schema\": ")...)
	out = append(out, schemaLine...)
	out = append(out, []byte(",")...)
	out = append(out, inner...)
	out = append(out, '}', '\n')
	_, err = w.Write(out)
	return err
}
