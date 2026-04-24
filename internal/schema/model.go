// Package schema introspects PostgreSQL system catalogs and exposes a
// typed, JSON-serialisable snapshot of the tables, indexes, and
// foreign keys visible to the current user. Rules use the snapshot
// via the lookup helpers to avoid suggesting indexes that already
// exist and to spot missing-index coverage on foreign keys.
package schema

import (
	"fmt"
	"regexp"
	"time"
)

// Schema is the top-level introspection result. Tables and Indexes
// are keyed by fully qualified names ("schema.name") for O(1) lookup.
type Schema struct {
	Meta    Meta              `json:"meta"`
	Tables  map[string]*Table `json:"tables"`
	Indexes map[string]*Index `json:"indexes"`
	FKeys   []ForeignKey      `json:"foreign_keys"`
}

// Meta records provenance for a schema snapshot. Without it, a JSON
// export is ambiguous about when it was taken and against which
// database.
type Meta struct {
	ExportedAt     time.Time `json:"exported_at"`
	Database       string    `json:"database"`
	ServerVersion  string    `json:"server_version"`
	DBAgentVersion string    `json:"dbagent_version"`
}

// Table is a single relation with its columns and maintenance stats.
// RowEstimate comes from pg_class.reltuples and is intentionally an
// estimate — callers that want exact counts must query the table
// themselves.
type Table struct {
	Schema       string    `json:"schema"`
	Name         string    `json:"name"`
	Columns      []Column  `json:"columns"`
	RowEstimate  int64     `json:"row_estimate"`
	SizeBytes    int64     `json:"size_bytes"`
	LastAnalyzed time.Time `json:"last_analyzed,omitempty"`
	LastVacuumed time.Time `json:"last_vacuumed,omitempty"`
}

// FQN returns "schema.name". Identifiers are double-quoted when they
// contain characters outside the safe set, matching PostgreSQL's own
// rules so the FQN can be pasted straight into SQL.
func (t *Table) FQN() string {
	if t == nil {
		return ""
	}
	return qualify(t.Schema, t.Name)
}

// Column describes one attribute of a Table. Position is 1-based to
// match pg_attribute.attnum.
type Column struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	NotNull  bool   `json:"not_null"`
	Default  string `json:"default,omitempty"`
	Position int    `json:"position"`
}

// Index describes a single index on a table. Columns is ordered as
// the index itself sees them; callers must not re-sort. For
// expression-based entries the introspection layer filters the NULL
// attnames out, so Columns contains only direct column references.
type Index struct {
	Schema     string   `json:"schema"`
	Name       string   `json:"name"`
	Table      string   `json:"table"` // schema-qualified: "public.orders"
	Columns    []string `json:"columns"`
	IsUnique   bool     `json:"is_unique"`
	IsPrimary  bool     `json:"is_primary"`
	IsPartial  bool     `json:"is_partial"`
	WhereExpr  string   `json:"where_expr,omitempty"`
	Method     string   `json:"method"`
	SizeBytes  int64    `json:"size_bytes"`
	Definition string   `json:"definition"`
}

// FQN returns "schema.name" for the index. See Table.FQN for quoting rules.
func (i *Index) FQN() string {
	if i == nil {
		return ""
	}
	return qualify(i.Schema, i.Name)
}

// ForeignKey is a single FOREIGN KEY constraint.
type ForeignKey struct {
	Name              string   `json:"name"`
	Schema            string   `json:"schema"`
	Table             string   `json:"table"`
	Columns           []string `json:"columns"`
	ReferencedSchema  string   `json:"referenced_schema"`
	ReferencedTable   string   `json:"referenced_table"`
	ReferencedColumns []string `json:"referenced_columns"`
}

// safeIdent matches identifiers PostgreSQL renders unquoted: starts
// with a letter or underscore, followed by letters, digits, or
// underscores, all lowercase.
var safeIdent = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// qualify joins a schema and name with a dot, quoting either piece
// when it contains non-safe characters.
func qualify(schema, name string) string {
	return quoteIfNeeded(schema) + "." + quoteIfNeeded(name)
}

// quoteIfNeeded returns ident unchanged when PostgreSQL would render
// it unquoted, otherwise wraps it in double quotes and escapes any
// embedded quotes.
func quoteIfNeeded(ident string) string {
	if ident == "" {
		return `""`
	}
	if safeIdent.MatchString(ident) {
		return ident
	}
	// Escape embedded quotes per PostgreSQL rules.
	var escaped string
	for _, r := range ident {
		if r == '"' {
			escaped += `""`
			continue
		}
		escaped += string(r)
	}
	return fmt.Sprintf(`"%s"`, escaped)
}
