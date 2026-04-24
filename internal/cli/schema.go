package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/byksy/dbagent/internal/config"
	"github.com/byksy/dbagent/internal/db"
	"github.com/byksy/dbagent/internal/schema"
	"github.com/spf13/cobra"
)

// newSchemaCmd builds the `dbagent schema` command and wires the
// `export` subcommand under it.
func newSchemaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Show database schema overview",
		Long: `Print a human-readable overview of the database schema: tables, indexes, and foreign keys.

Useful for verifying that dbagent sees the schema correctly before running analyze.
For a machine-readable export, use 'dbagent schema export' instead.`,
		RunE: runSchema,
	}
	cmd.AddCommand(newSchemaExportCmd())
	return cmd
}

// newSchemaExportCmd builds `dbagent schema export`.
func newSchemaExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Export schema as JSON for offline analysis",
		Long: `Write the database schema as JSON to stdout. Pipe to a file to save it
for later use with 'dbagent analyze --schema <file>'.

Example:
  dbagent schema export > schema.json
  dbagent analyze --plan-file plan.json --schema schema.json`,
		RunE: runSchemaExport,
	}
}

// fetchSchemaForCmd is the shared "load config → connect → Fetch"
// path used by both schema commands. Returns a typed ExitError on
// failure so main.go can surface the right exit code.
func fetchSchemaForCmd(cmd *cobra.Command) (*schema.Schema, error) {
	path := resolvedConfigPath()
	if path == "" {
		return nil, newExitError(ExitInternal, errors.New("cannot resolve default config path"))
	}
	cfg, err := config.Load(path)
	if err != nil {
		if errors.Is(err, config.ErrConfigNotFound) {
			return nil, newExitError(ExitNoConfig, fmt.Errorf("no config found, run 'dbagent init' first"))
		}
		return nil, newExitError(ExitUsageError, err)
	}
	ctx := cmd.Context()
	pool, err := db.Connect(ctx, cfg.Database)
	if err != nil {
		return nil, newExitError(ExitConnFailed, err)
	}
	defer pool.Close()

	s, err := schema.Fetch(ctx, pool)
	if err != nil {
		return nil, newExitError(ExitInternal, err)
	}
	s.Meta.DBAgentVersion = version
	return s, nil
}

// runSchema prints a human-readable three-section overview. Table
// widths are driven by tabwriter with a small padding so the output
// stays legible on an 80-col terminal.
func runSchema(cmd *cobra.Command, _ []string) error {
	s, err := fetchSchemaForCmd(cmd)
	if err != nil {
		return err
	}
	writeSchemaOverview(cmd.OutOrStdout(), s)
	return nil
}

// runSchemaExport writes the fetched schema as pretty JSON. The
// exported document is self-contained — consumers can load it with
// schema.LoadJSON on any machine.
func runSchemaExport(cmd *cobra.Command, _ []string) error {
	s, err := fetchSchemaForCmd(cmd)
	if err != nil {
		return err
	}
	if err := s.WriteJSON(cmd.OutOrStdout()); err != nil {
		return newExitError(ExitInternal, err)
	}
	return nil
}

// writeSchemaOverview renders the three sections plus a leading
// line of metadata. Output targets a terminal; for machine-readable
// output use `schema export`.
func writeSchemaOverview(w io.Writer, s *schema.Schema) {
	fmt.Fprintf(w, "Database: %s (PostgreSQL %s)\n\n", s.Meta.Database, s.Meta.ServerVersion)
	writeTablesSection(w, s)
	fmt.Fprintln(w)
	writeIndexesSection(w, s)
	fmt.Fprintln(w)
	writeFKeysSection(w, s)
}

func writeTablesSection(w io.Writer, s *schema.Schema) {
	tables := make([]*schema.Table, 0, len(s.Tables))
	for _, t := range s.Tables {
		tables = append(tables, t)
	}
	sort.SliceStable(tables, func(i, j int) bool {
		if tables[i].Schema != tables[j].Schema {
			return tables[i].Schema < tables[j].Schema
		}
		return tables[i].Name < tables[j].Name
	})

	fmt.Fprintf(w, "Tables (%d total)\n", len(tables))
	tw := tabwriter.NewWriter(w, 2, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "  schema\tname\trows\tsize\tindexes\tfkeys")
	for _, t := range tables {
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%d\t%d\n",
			t.Schema, t.Name,
			formatCount(t.RowEstimate),
			formatBytes(t.SizeBytes),
			countIndexesOn(s, t.FQN()),
			countFKeysOn(s, t.FQN()),
		)
	}
	_ = tw.Flush()
}

func writeIndexesSection(w io.Writer, s *schema.Schema) {
	indexes := make([]*schema.Index, 0, len(s.Indexes))
	for _, idx := range s.Indexes {
		indexes = append(indexes, idx)
	}
	sort.SliceStable(indexes, func(i, j int) bool {
		if indexes[i].Table != indexes[j].Table {
			return indexes[i].Table < indexes[j].Table
		}
		return indexes[i].Name < indexes[j].Name
	})

	fmt.Fprintf(w, "Indexes (%d total)\n", len(indexes))
	tw := tabwriter.NewWriter(w, 2, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "  table\tname\tcolumns\tunique\tsize")
	for _, idx := range indexes {
		mark := ""
		if idx.IsUnique {
			mark = "✓"
		}
		cols := "(" + joinComma(idx.Columns) + ")"
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
			idx.Table, idx.Name, cols, mark, formatBytes(idx.SizeBytes))
	}
	_ = tw.Flush()
}

func writeFKeysSection(w io.Writer, s *schema.Schema) {
	fks := make([]schema.ForeignKey, len(s.FKeys))
	copy(fks, s.FKeys)
	sort.SliceStable(fks, func(i, j int) bool {
		if fks[i].Schema != fks[j].Schema {
			return fks[i].Schema < fks[j].Schema
		}
		if fks[i].Table != fks[j].Table {
			return fks[i].Table < fks[j].Table
		}
		return fks[i].Name < fks[j].Name
	})

	fmt.Fprintf(w, "Foreign keys (%d total)\n", len(fks))
	tw := tabwriter.NewWriter(w, 2, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "  table\tcolumn(s)\t→ references")
	for _, fk := range fks {
		fmt.Fprintf(tw, "  %s.%s\t%s\t→ %s.%s.%s\n",
			fk.Schema, fk.Table,
			joinComma(fk.Columns),
			fk.ReferencedSchema, fk.ReferencedTable, joinComma(fk.ReferencedColumns))
	}
	_ = tw.Flush()
}

func countIndexesOn(s *schema.Schema, tableFQN string) int {
	n := 0
	for _, idx := range s.Indexes {
		if idx.Table == tableFQN {
			n++
		}
	}
	return n
}

func countFKeysOn(s *schema.Schema, tableFQN string) int {
	n := 0
	for _, fk := range s.FKeys {
		if fk.Schema+"."+fk.Table == tableFQN {
			n++
		}
	}
	return n
}

// formatBytes renders a byte count with one decimal: "256.0 kB",
// "12.5 MB", "1.8 GB". Everything above MB uses GB.
func formatBytes(n int64) string {
	const kB = 1024
	const mB = 1024 * kB
	const gB = 1024 * mB
	switch {
	case n >= gB:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gB))
	case n >= mB:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mB))
	case n >= kB:
		return fmt.Sprintf("%.1f kB", float64(n)/float64(kB))
	}
	return fmt.Sprintf("%d B", n)
}

// joinComma is a tiny helper so the section builders don't all
// import strings just for a single join.
func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

