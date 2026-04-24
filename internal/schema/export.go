package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// StalenessThreshold is the age above which a loaded Schema is
// considered out of date. 24 hours is a compromise: schemas rarely
// change hourly but often change daily in active projects, so
// anything older gets a warning without being a hard error.
const StalenessThreshold = 24 * time.Hour

// ErrStaleSchema is returned by LoadJSON (wrapped) when the schema
// export is older than StalenessThreshold. Callers can use
// errors.Is to detect it and surface a warning while still using
// the returned Schema.
var ErrStaleSchema = errors.New("schema export is older than " + StalenessThreshold.String())

// WriteJSON writes s to w as pretty-printed JSON (two-space indent).
// Meta is expected to be populated by the caller; WriteJSON does not
// modify it.
func (s *Schema) WriteJSON(w io.Writer) error {
	if s == nil {
		return errors.New("schema: WriteJSON on nil")
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return fmt.Errorf("schema: encode JSON: %w", err)
	}
	return nil
}

// LoadJSON parses a schema JSON document from r. Returns the parsed
// Schema AND ErrStaleSchema (wrapped) when the export is older than
// the threshold — callers must surface the warning but should not
// refuse to proceed. For any other parse error, the returned Schema
// is nil.
func LoadJSON(r io.Reader) (*Schema, error) {
	dec := json.NewDecoder(r)
	s := &Schema{}
	if err := dec.Decode(s); err != nil {
		return nil, fmt.Errorf("schema: decode JSON: %w", err)
	}
	if s.Tables == nil {
		s.Tables = map[string]*Table{}
	}
	if s.Indexes == nil {
		s.Indexes = map[string]*Index{}
	}
	if s.IsStale() {
		return s, fmt.Errorf("%w: exported %s ago", ErrStaleSchema, s.StaleAge().Round(time.Hour))
	}
	return s, nil
}

// IsStale reports whether the schema was exported more than
// StalenessThreshold ago. Returns false if ExportedAt is the zero
// value (the JSON predates versioning).
func (s *Schema) IsStale() bool {
	if s == nil || s.Meta.ExportedAt.IsZero() {
		return false
	}
	return s.StaleAge() > StalenessThreshold
}

// StaleAge returns how long ago the schema was exported. Returns 0
// when ExportedAt is zero.
func (s *Schema) StaleAge() time.Duration {
	if s == nil || s.Meta.ExportedAt.IsZero() {
		return 0
	}
	return time.Since(s.Meta.ExportedAt)
}
