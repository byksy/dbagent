package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/byksy/dbagent/internal/plan"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// analyzeFlags holds the per-invocation flag values for "analyze".
type analyzeFlags struct {
	planFile string
	format   string
	width    int
}

// newAnalyzeCmd builds the "analyze" subcommand.
func newAnalyzeCmd() *cobra.Command {
	f := &analyzeFlags{}
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Parse and render an EXPLAIN (FORMAT JSON) plan",
		Long: `Parse a PostgreSQL execution plan produced by EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
and render it as a tree or table with a summary of notable nodes.

This version is offline-only: the plan must be provided via --plan-file or stdin.
Live analysis via --queryid or --sql will come in a future release.

Examples:
  dbagent analyze --plan-file plan.json
  cat plan.json | dbagent analyze
  dbagent analyze --plan-file plan.json --format table
  dbagent analyze --plan-file plan.json --format json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAnalyze(cmd, f)
		},
	}
	cmd.Flags().StringVar(&f.planFile, "plan-file", "", "path to EXPLAIN JSON file; empty means read from stdin")
	cmd.Flags().StringVar(&f.format, "format", "tree", "output format: tree|table|json")
	cmd.Flags().IntVar(&f.width, "width", 0, "override terminal width; 0 = auto-detect")
	return cmd
}

// runAnalyze orchestrates reading, parsing, summarising, and rendering.
func runAnalyze(cmd *cobra.Command, f *analyzeFlags) error {
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
	w := cmd.OutOrStdout()

	width := f.width
	if width <= 0 {
		width = renderWidth()
	}

	switch f.format {
	case "", "tree":
		return renderTree(w, p, summary, width)
	case "table":
		return renderTable(w, p, summary, width)
	case "json":
		return renderJSON(w, p, summary)
	default:
		return newExitError(ExitUsageError, fmt.Errorf("invalid --format %q, expected tree|table|json", f.format))
	}
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

// renderWidth returns the current terminal width or a reasonable
// fallback. Mirrors terminalWidth() in format.go but is kept separate
// here so Stage 1's top output isn't pulled in transitively.
func renderWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 120
	}
	return w
}
