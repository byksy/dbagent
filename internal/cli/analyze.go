package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/byksy/dbagent/internal/plan"
	"github.com/byksy/dbagent/internal/rules"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// analyzeFlags holds the per-invocation flag values for "analyze".
type analyzeFlags struct {
	planFile string
	format   string
	failOn   string
}

// newAnalyzeCmd builds the "analyze" subcommand.
func newAnalyzeCmd() *cobra.Command {
	f := &analyzeFlags{}
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Parse and render an EXPLAIN (FORMAT JSON) plan",
		Long: `Parse a PostgreSQL execution plan produced by EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON),
render it, and run diagnostic/prescriptive rules.

This version is offline-only: the plan must be provided via --plan-file or stdin.
Live analysis via --queryid or --sql will come in a future release.

Examples:
  dbagent analyze --plan-file plan.json
  cat plan.json | dbagent analyze
  dbagent analyze --plan-file plan.json --format table
  dbagent analyze --plan-file plan.json --format json
  dbagent analyze --plan-file plan.json --fail-on warning`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAnalyze(cmd, f)
		},
	}
	cmd.Flags().StringVar(&f.planFile, "plan-file", "", "path to EXPLAIN JSON file; empty means read from stdin")
	cmd.Flags().StringVar(&f.format, "format", "tree", "output format: tree|table|json")
	cmd.Flags().StringVar(&f.failOn, "fail-on", "", "exit non-zero if any finding reaches this severity: info|warning|critical")
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

	summary := plan.Summarize(p)
	findings := rules.Run(p, rules.Default())
	w := cmd.OutOrStdout()

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
