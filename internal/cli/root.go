// Package cli wires up the cobra command tree for dbagent.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Exit codes. Each command returns an *ExitError carrying one of these
// when it wants to override the default cobra exit code of 1.
const (
	ExitOK           = 0
	ExitUsageError   = 1
	ExitNoConfig     = 2
	ExitExtNotReady  = 3
	ExitConnFailed   = 4
	ExitInternal     = 5
)

// ExitError is a typed error that carries a process exit code, so that
// main.go can translate a command failure into the right exit status
// without re-encoding error categories as strings.
type ExitError struct {
	Code int
	Err  error
}

// Error implements error by delegating to the wrapped error.
func (e *ExitError) Error() string {
	if e == nil || e.Err == nil {
		return "exit error"
	}
	return e.Err.Error()
}

// Unwrap exposes the wrapped error for errors.Is / errors.As.
func (e *ExitError) Unwrap() error { return e.Err }

// newExitError wraps err with the given exit code. Returns nil if err
// is nil so callers can "return newExitError(code, doThing())".
func newExitError(code int, err error) error {
	if err == nil {
		return nil
	}
	return &ExitError{Code: code, Err: err}
}

// loggerKey is the context key under which the command logger is
// stored. Unexported so callers must go through Logger.
type loggerKey struct{}

// Logger retrieves the slog.Logger stored on the cobra command's
// context. Falls back to slog.Default if nothing was set.
func Logger(cmd *cobra.Command) *slog.Logger {
	if cmd == nil {
		return slog.Default()
	}
	if v := cmd.Context().Value(loggerKey{}); v != nil {
		if l, ok := v.(*slog.Logger); ok {
			return l
		}
	}
	return slog.Default()
}

// persistent flag holders.
var (
	flagConfigPath string
	flagLogLevel   string
	flagVerbose    bool
)

// Execute builds the root command tree and runs it, returning any
// error produced by the command (so main.go can inspect ExitError).
func Execute() error {
	root := newRootCmd()
	return root.Execute()
}

// newRootCmd constructs the root cobra command with subcommands.
func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dbagent",
		Short: "PostgreSQL query analyzer",
		Long: `dbagent is a CLI tool for analyzing PostgreSQL query performance.
It reads pg_stat_statements and (in future versions) parses EXPLAIN
output to suggest optimizations.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(c *cobra.Command, _ []string) error {
			level := resolveLogLevel(flagLogLevel, flagVerbose)
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
			ctx := context.WithValue(c.Context(), loggerKey{}, logger)
			c.SetContext(ctx)
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&flagConfigPath, "config", "", "path to config file (default: $XDG_CONFIG_HOME/dbagent/config.yaml)")
	cmd.PersistentFlags().StringVar(&flagLogLevel, "log-level", "", "log level: debug|info|warn|error (default: from config or info)")
	cmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose output (implies --log-level=debug for logging)")

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newTopCmd())
	cmd.AddCommand(newVersionCmd())

	return cmd
}

// resolveLogLevel converts a user-facing level string (plus a verbose
// flag) into a slog.Level.
func resolveLogLevel(name string, verbose bool) slog.Level {
	if verbose {
		return slog.LevelDebug
	}
	switch strings.ToLower(name) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "", "info":
		return slog.LevelInfo
	}
	return slog.LevelInfo
}

// usageErrorf is a helper for RunE to return a usage-class error.
func usageErrorf(format string, args ...any) error {
	return newExitError(ExitUsageError, fmt.Errorf(format, args...))
}
