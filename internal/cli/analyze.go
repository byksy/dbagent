package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/byksy/dbagent/internal/config"
	"github.com/byksy/dbagent/internal/db"
	"github.com/byksy/dbagent/internal/plan"
	"github.com/byksy/dbagent/internal/rules"
	"github.com/byksy/dbagent/internal/schema"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// analyzeFlags holds the per-invocation flag values for "analyze".
// schemaPath uses a pointer so we can distinguish "unset" from
// "explicitly empty string" — the latter opts out of fetching.
type analyzeFlags struct {
	planFile   string
	format     string
	failOn     string
	schemaPath *string
}

// newAnalyzeCmd builds the "analyze" subcommand.
func newAnalyzeCmd() *cobra.Command {
	f := &analyzeFlags{}
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Parse and render an EXPLAIN (FORMAT JSON) plan",
		Long: `Parse a PostgreSQL execution plan produced by EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON),
render it, and run diagnostic/prescriptive rules.

Analysis is offline — the plan comes from --plan-file or stdin. When config is
present and the DB is reachable, dbagent fetches the live schema automatically
so rules can see existing indexes. Use --schema to load a JSON export instead
(produced by 'dbagent schema export').

Examples:
  dbagent analyze --plan-file plan.json
  cat plan.json | dbagent analyze
  dbagent analyze --plan-file plan.json --format table
  dbagent analyze --plan-file plan.json --format json
  dbagent analyze --plan-file plan.json --schema schema.json
  dbagent analyze --plan-file plan.json --fail-on warning`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAnalyze(cmd, f)
		},
	}
	cmd.Flags().StringVar(&f.planFile, "plan-file", "", "path to EXPLAIN JSON file; empty means read from stdin")
	cmd.Flags().StringVar(&f.format, "format", "tree", "output format: tree|table|json")
	cmd.Flags().StringVar(&f.failOn, "fail-on", "", "exit non-zero if any finding reaches this severity: info|warning|critical")

	// --schema: pointer-tracked so we can tell "user passed --schema \"\""
	// (explicit offline) apart from "user didn't pass it at all" (try to
	// fetch live schema when a config is available).
	var schemaPath string
	cmd.Flags().StringVar(&schemaPath, "schema", "", "path to schema.json for offline analysis; overrides live fetch. Pass --schema= to opt out of fetching")
	cobra.OnInitialize(func() {
		if cmd.Flags().Changed("schema") {
			f.schemaPath = &schemaPath
		}
	})
	// Can't rely on OnInitialize firing before RunE (cobra runs it
	// during Execute but only once per process), so also snapshot in
	// PreRunE for reliability across test invocations.
	cmd.PreRunE = func(c *cobra.Command, _ []string) error {
		if c.Flags().Changed("schema") {
			f.schemaPath = &schemaPath
		}
		return nil
	}
	return cmd
}

// runAnalyze orchestrates reading, parsing, running rules, and
// rendering. If --fail-on is set and at least one finding meets the
// threshold, the command exits with ExitFindingsAboveThreshold.
func runAnalyze(cmd *cobra.Command, f *analyzeFlags) error {
	var failOnThreshold rules.Severity
	var failOnSet bool
	if f.failOn != "" {
		sev, err := rules.ParseSeverity(f.failOn)
		if err != nil {
			return newExitError(ExitUsageError, err)
		}
		failOnThreshold = sev
		failOnSet = true
	}

	reader, source, closer, err := openPlanInput(cmd, f.planFile)
	if err != nil {
		return newExitError(ExitUsageError, err)
	}
	if closer != nil {
		defer closer()
	}

	p, err := plan.Parse(reader)
	if err != nil {
		switch {
		case errors.Is(err, plan.ErrEmptyPlan),
			errors.Is(err, plan.ErrInvalidJSON),
			errors.Is(err, plan.ErrUnsupportedFormat):
			return newExitError(ExitParseFailed, err)
		default:
			return newExitError(ExitInternal, err)
		}
	}
	p.SourceDescription = source

	w := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

	sch, banner, err := resolveSchema(cmd, f)
	if err != nil {
		return err
	}
	if banner != "" {
		fmt.Fprintln(w, banner)
		fmt.Fprintln(w)
	}
	_ = stderr // reserved for future non-fatal warnings

	summary := plan.Summarize(p)
	findings := rules.Run(&rules.RuleContext{Plan: p, Schema: sch}, rules.Default())

	switch f.format {
	case "", "tree":
		if err := renderTree(w, p, summary, findings); err != nil {
			return err
		}
	case "table":
		if err := renderTable(w, p, summary, findings); err != nil {
			return err
		}
	case "json":
		if err := renderJSON(w, p, summary, findings); err != nil {
			return err
		}
	default:
		return newExitError(ExitUsageError, fmt.Errorf("invalid --format %q, expected tree|table|json", f.format))
	}

	if failOnSet {
		for _, fd := range findings {
			if fd.Severity >= failOnThreshold {
				return newExitError(ExitFindingsAboveThreshold,
					fmt.Errorf("%d finding(s) at or above %s", countAtOrAbove(findings, failOnThreshold), failOnThreshold))
			}
		}
	}
	return nil
}

// resolveSchema applies the layered rules for deciding where the
// Schema comes from:
//
//  1. --schema <path>: load from file; usage error if missing.
//  2. --schema "" explicit: skip schema entirely (pure offline).
//  3. config present AND DB reachable: fetch from live DB.
//  4. otherwise: nil schema; analyze still runs.
//
// The optional banner string is printed at the top of the analysis
// output when schema is missing or stale, so the user sees exactly
// what the rules had to work with.
func resolveSchema(cmd *cobra.Command, f *analyzeFlags) (*schema.Schema, string, error) {
	if f.schemaPath != nil {
		path := *f.schemaPath
		if path == "" {
			return nil, noSchemaBanner, nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, "", newExitError(ExitUsageError, fmt.Errorf("open --schema %s: %w", path, err))
		}
		defer file.Close()
		s, err := schema.LoadJSON(file)
		if err != nil && !errors.Is(err, schema.ErrStaleSchema) {
			return nil, "", newExitError(ExitUsageError, err)
		}
		if errors.Is(err, schema.ErrStaleSchema) {
			days := int(s.StaleAge().Hours() / 24)
			return s, fmt.Sprintf("(schema export is %d days old; results may be inaccurate)", days), nil
		}
		return s, "", nil
	}

	// No --schema flag — try live fetch if config looks available.
	s, err := tryFetchSchema(cmd)
	if err != nil || s == nil {
		return nil, noSchemaBanner, nil
	}
	return s, "", nil
}

const noSchemaBanner = "(schema not loaded; rule suggestions may reference already-indexed columns)"

// tryFetchSchema attempts a best-effort schema load from the live
// DB. Every failure path returns nil — analyze must still work for
// users who don't have DB access.
func tryFetchSchema(cmd *cobra.Command) (*schema.Schema, error) {
	path := resolvedConfigPath()
	if path == "" {
		return nil, nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer cancel()

	pool, err := db.Connect(ctx, cfg.Database)
	if err != nil {
		return nil, nil
	}
	defer pool.Close()

	s, err := schema.Fetch(ctx, pool)
	if err != nil {
		return nil, nil
	}
	return s, nil
}

// countAtOrAbove returns the number of findings at or above thr. Used
// to produce a more informative exit-error message than a bare flag.
func countAtOrAbove(findings []rules.Finding, thr rules.Severity) int {
	n := 0
	for _, f := range findings {
		if f.Severity >= thr {
			n++
		}
	}
	return n
}

// openPlanInput resolves the analyze command's input source. Returns
// the reader, a human-readable source description, and an optional
// closer. Errors when stdin is a TTY and no file was given.
func openPlanInput(cmd *cobra.Command, path string) (io.Reader, string, func(), error) {
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return nil, "", nil, fmt.Errorf("open %s: %w", path, err)
		}
		return f, path, func() { _ = f.Close() }, nil
	}
	stdin := cmd.InOrStdin()
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return nil, "", nil, errors.New("no input: pass --plan-file <path> or pipe plan JSON to stdin")
	}
	return stdin, "<stdin>", nil, nil
}
