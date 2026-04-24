package cli

import (
	"errors"
	"fmt"

	"github.com/byksy/dbagent/internal/config"
	"github.com/byksy/dbagent/internal/db"
	"github.com/byksy/dbagent/internal/pgstat"
	"github.com/byksy/dbagent/internal/stats"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
)

// statsFlags captures the per-invocation options for `dbagent stats`.
type statsFlags struct {
	format        string
	topN          int
	since         int
	exclude       []string
	noColor       bool
	includeSystem bool
}

// newStatsCmd builds the `dbagent stats` subcommand. It is the first
// command that renders with lipgloss and exercises the style package;
// nothing else here touches existing analyze/top/init code paths.
func newStatsCmd() *cobra.Command {
	f := &statsFlags{}
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Workload-level analysis from pg_stat_statements",
		Long: `Analyze database workload patterns — where time is spent, which queries dominate, how cache is performing. Unlike 'analyze', which examines a single query plan, stats looks at the database as a whole.

Requires pg_stat_statements to be enabled. Run 'dbagent init --check' to verify.

Examples:
  dbagent stats                                # default: terminal output
  dbagent stats --format html > report.html    # shareable HTML
  dbagent stats --format json                  # structured for CI/scripts
  dbagent stats --top 20                       # more rows per section`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStats(cmd, f)
		},
	}
	cmd.Flags().StringVar(&f.format, "format", "terminal", "output format: terminal|html|json")
	cmd.Flags().IntVar(&f.topN, "top", 10, "rows per section (max 50)")
	// --since is accepted for forward compatibility. pg_stat_statements
	// has no per-statement "last_seen" column, so a true rolling window
	// can't be enforced here; the flag is plumbed to stats.Options so
	// callers can grow a real filter once PostgreSQL exposes one.
	cmd.Flags().IntVar(&f.since, "since", 0, "accepted for compatibility; pg_stat_statements does not expose per-statement age, so the value is not applied as a rolling filter")
	cmd.Flags().StringSliceVar(&f.exclude, "exclude", nil, "exclude queries matching these regex patterns")
	cmd.Flags().BoolVar(&f.noColor, "no-color", false, "force no-color output (NO_COLOR env var also respected)")
	cmd.Flags().BoolVar(&f.includeSystem, "include-system", false, "include pg_catalog / SET / SHOW / VACUUM / ANALYZE queries (excluded by default)")
	return cmd
}

// runStats orchestrates config load, DB connection, extension check,
// workload fetch, and dispatch to the selected renderer.
func runStats(cmd *cobra.Command, f *statsFlags) error {
	if f.topN <= 0 {
		f.topN = 10
	}
	if f.topN > 50 {
		f.topN = 50
	}
	// --no-color hard-clamps the profile before any style is rendered.
	// NO_COLOR / non-TTY detection still happen inside lipgloss'
	// defaults; this flag only adds a manual override so users can
	// strip colours without touching their environment.
	if f.noColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	path := resolvedConfigPath()
	if path == "" {
		return newExitError(ExitInternal, errors.New("cannot resolve default config path"))
	}
	cfg, err := config.Load(path)
	if err != nil {
		if errors.Is(err, config.ErrConfigNotFound) {
			return newExitError(ExitNoConfig, fmt.Errorf("no config found, run 'dbagent init' first"))
		}
		return newExitError(ExitUsageError, err)
	}

	ctx := cmd.Context()
	pool, err := db.Connect(ctx, cfg.Database)
	if err != nil {
		return newExitError(ExitConnFailed, err)
	}
	defer pool.Close()

	status, err := pgstat.CheckExtension(ctx, pool)
	if err != nil {
		return newExitError(ExitInternal, err)
	}
	if !status.Ready() {
		return newExitError(ExitExtNotReady,
			errors.New("pg_stat_statements is not ready, run 'dbagent init --check' for details"))
	}

	ws, err := stats.Compute(ctx, pool, stats.Options{
		TopN:          f.topN,
		SinceMinutes:  f.since,
		ExcludeRegexp: f.exclude,
		IncludeSystem: f.includeSystem,
	})
	if err != nil {
		return newExitError(ExitInternal, err)
	}
	ws.Meta.DBAgentVersion = resolvedVersion()

	w := cmd.OutOrStdout()
	switch f.format {
	case "", "terminal":
		return renderStatsTerminal(w, ws, 0)
	case "json":
		return renderStatsJSON(w, ws)
	case "html":
		return renderStatsHTML(w, ws)
	}
	return newExitError(ExitUsageError, fmt.Errorf("invalid --format %q, expected terminal|html|json", f.format))
}
