package cli

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/byksy/dbagent/internal/config"
	"github.com/byksy/dbagent/internal/db"
	"github.com/byksy/dbagent/internal/pgstat"
	"github.com/spf13/cobra"
)

// topFlags holds the "top" command's flag values.
type topFlags struct {
	limit         int
	orderBy       string
	includeSystem bool
}

// newTopCmd builds the "top" subcommand.
func newTopCmd() *cobra.Command {
	f := &topFlags{}
	cmd := &cobra.Command{
		Use:   "top",
		Short: "Show the top queries from pg_stat_statements",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTop(cmd, f)
		},
	}
	cmd.Flags().IntVar(&f.limit, "limit", 0, "number of queries to show (default: from config)")
	cmd.Flags().StringVar(&f.orderBy, "order-by", "", "order by: total|mean|calls|io|cache (default: from config)")
	cmd.Flags().BoolVar(&f.includeSystem, "include-system", false, "include pg_catalog / SET / SHOW / VACUUM / ANALYZE queries (excluded by default)")
	return cmd
}

func runTop(cmd *cobra.Command, f *topFlags) error {
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

	opts := pgstat.TopOptions{
		Limit:         cfg.Output.Limit,
		OrderBy:       cfg.Output.OrderBy,
		IncludeSystem: f.includeSystem,
	}
	if f.limit != 0 {
		opts.Limit = f.limit
	}
	if f.orderBy != "" {
		opts.OrderBy = f.orderBy
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

	stats, err := pgstat.TopQueries(ctx, pool, opts)
	if err != nil {
		if errors.Is(err, pgstat.ErrExtensionMissing) {
			return newExitError(ExitExtNotReady, err)
		}
		return newExitError(ExitInternal, err)
	}

	printTopTable(cmd.OutOrStdout(), stats, flagVerbose)
	return nil
}

// printTopTable renders the query stats as a padded table. The two
// columns of extra detail (rows, cache hit%, blks read) are included
// when verbose is true.
func printTopTable(w io.Writer, stats []pgstat.QueryStat, verbose bool) {
	tw := tabwriter.NewWriter(w, 2, 2, 2, ' ', 0)

	// Total-time denominator for the % of total column. We use the sum
	// across the *returned* rows, not the whole pg_stat_statements view,
	// so the percentages in the table always add up to 100%. This is a
	// choice, not arbitrary — a whole-view denominator would understate
	// the visible rows' share.
	var totalTime float64
	var totalCalls int64
	for _, s := range stats {
		totalTime += s.TotalExecTime
		totalCalls += s.Calls
	}

	queryCol := queryColumnWidth(verbose)

	if verbose {
		fmt.Fprintln(tw, "  #\tcalls\tmean\ttotal\t% total\trows\tcache hit%\tblks read\tquery")
	} else {
		fmt.Fprintln(tw, "  #\tcalls\tmean\ttotal\t% of total\tquery")
	}

	for i, s := range stats {
		q := truncateQuery(s.Query, queryCol)
		if verbose {
			fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				i+1,
				formatCount(s.Calls),
				formatDuration(s.MeanExecTime),
				formatDuration(s.TotalExecTime),
				formatPercent(s.TotalExecTime, totalTime),
				formatCount(s.Rows),
				formatCacheHitPct(s.SharedBlksHit, s.SharedBlksRead),
				formatCount(s.SharedBlksRead),
				q,
			)
		} else {
			fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\t%s\n",
				i+1,
				formatCount(s.Calls),
				formatDuration(s.MeanExecTime),
				formatDuration(s.TotalExecTime),
				formatPercent(s.TotalExecTime, totalTime),
				q,
			)
		}
	}
	_ = tw.Flush()

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Total captured: %s across %s executions\n",
		formatDuration(totalTime), formatCount(totalCalls))
	fmt.Fprintf(w, "  Source: pg_stat_statements (snapshot at %s)\n",
		time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))
}

// queryColumnWidth returns a reasonable width for the free-form query
// column. Verbose mode has three extra columns, so the query gets less
// room.
func queryColumnWidth(verbose bool) int {
	// Rough fixed-column-width estimates, tuned so that a typical SELECT
	// is still legible on an 80-col terminal.
	fixed := 55 // "  #  calls  mean  total  % of total  " plus padding
	if verbose {
		fixed = 95
	}
	w := terminalWidth() - fixed
	if w < 20 {
		return 20
	}
	return w
}
