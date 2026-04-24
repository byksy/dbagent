package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/byksy/dbagent/internal/config"
	"github.com/byksy/dbagent/internal/db"
	"github.com/byksy/dbagent/internal/pgstat"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// initFlags holds the raw flag values for the init command. Kept in a
// struct (rather than package-level vars) so each cobra command
// construction has its own clean state.
type initFlags struct {
	host        string
	port        int
	database    string
	user        string
	password    string
	passwordEnv string
	sslmode     string
	noPrompt    bool
	check       bool
	force       bool
}

// newInitCmd builds the "init" subcommand.
func newInitCmd() *cobra.Command {
	f := &initFlags{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create or verify dbagent's database configuration",
		Long: `Initialise dbagent's configuration by collecting connection details,
testing the connection, and checking that pg_stat_statements is
available. With --check, reads the existing config and reports status
without modifying anything.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if f.check {
				return runInitCheck(cmd)
			}
			return runInit(cmd, f)
		},
	}

	cmd.Flags().StringVar(&f.host, "host", "", "database host")
	cmd.Flags().IntVar(&f.port, "port", 0, "database port")
	cmd.Flags().StringVar(&f.database, "database", "", "database name")
	cmd.Flags().StringVar(&f.user, "user", "", "database user")
	cmd.Flags().StringVar(&f.password, "password", "", "database password (prefer --password-env)")
	cmd.Flags().StringVar(&f.passwordEnv, "password-env", "", "environment variable holding the database password")
	cmd.Flags().StringVar(&f.sslmode, "sslmode", "", "sslmode: disable|require|verify-ca|verify-full")
	cmd.Flags().BoolVar(&f.noPrompt, "no-prompt", false, "fail instead of prompting for missing values")
	cmd.Flags().BoolVar(&f.check, "check", false, "check existing config, do not modify")
	cmd.Flags().BoolVar(&f.force, "force", false, "overwrite an existing config without prompting")

	return cmd
}

// runInitCheck loads the existing config, connects, and reports the
// pg_stat_statements status without modifying anything.
func runInitCheck(cmd *cobra.Command) error {
	path := resolvedConfigPath()
	if path == "" {
		return newExitError(ExitInternal, errors.New("cannot resolve default config path"))
	}

	cfg, err := config.Load(path)
	if err != nil {
		if errors.Is(err, config.ErrConfigNotFound) {
			return newExitError(ExitNoConfig, fmt.Errorf("no config found at %s, run 'dbagent init' first", path))
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

	printExtensionStatus(cmd.OutOrStdout(), status, cfg.Database.Database)
	if !status.Ready() {
		return newExitError(ExitExtNotReady, errors.New("pg_stat_statements is not ready"))
	}
	return nil
}

// runInit walks the interactive (or flag-driven) setup flow.
func runInit(cmd *cobra.Command, f *initFlags) error {
	if f.password != "" {
		fmt.Fprintln(cmd.ErrOrStderr(),
			"warning: --password exposes the password via process listings; prefer --password-env.")
	}

	path := resolvedConfigPath()
	if path == "" {
		return newExitError(ExitInternal, errors.New("cannot resolve default config path"))
	}

	existing, _ := config.Load(path)
	defaults := config.Default()
	if existing != nil {
		defaults = existing
		proceed, err := guardOverwrite(cmd, f, path)
		if err != nil {
			return err
		}
		if !proceed {
			return nil
		}
	}

	cfg, err := collectConfig(cmd, f, defaults)
	if err != nil {
		return newExitError(ExitUsageError, err)
	}
	if err := cfg.Validate(); err != nil {
		return newExitError(ExitUsageError, err)
	}

	ctx := cmd.Context()
	pool, connErr := db.Connect(ctx, cfg.Database)
	if connErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Could not connect to the database: %v\n", connErr)
		if f.noPrompt {
			return newExitError(ExitConnFailed, connErr)
		}
		if shouldSaveAnyway(cmd) {
			if err := config.Save(cfg, path); err != nil {
				return newExitError(ExitInternal, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Config saved to %s\n", path)
			fmt.Fprintln(cmd.OutOrStdout(), "Fix the database connection and re-run 'dbagent init --check'.")
		}
		return newExitError(ExitConnFailed, connErr)
	}
	defer pool.Close()

	fmt.Fprintf(cmd.OutOrStdout(), "Connected to PostgreSQL.\n")

	status, err := pgstat.CheckExtension(ctx, pool)
	if err != nil {
		return newExitError(ExitInternal, err)
	}
	printExtensionStatus(cmd.OutOrStdout(), status, cfg.Database.Database)

	if err := config.Save(cfg, path); err != nil {
		return newExitError(ExitInternal, err)
	}
	printFinalMessage(cmd.OutOrStdout(), path, status.Ready())
	return nil
}

// guardOverwrite decides whether an existing config at path may be
// clobbered. --force always wins. In non-interactive mode (--no-prompt
// or stdin is not a TTY) we refuse so automation can't silently
// replace a good config. In interactive mode we prompt; declining
// exits cleanly (proceed=false, err=nil) so repeated invocations
// from a script aren't punished with an error status.
func guardOverwrite(cmd *cobra.Command, f *initFlags, path string) (bool, error) {
	if f.force {
		return true, nil
	}
	if f.noPrompt || !isTerminal(os.Stdin) {
		return false, newExitError(ExitUsageError,
			fmt.Errorf("config already exists at %s; re-run with --force to overwrite", path))
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "A config already exists at %s.\n", path)
	ok, err := confirm(cmd.InOrStdin(), cmd.ErrOrStderr(), "Overwrite it? (y/N):")
	if err != nil {
		return false, newExitError(ExitInternal, err)
	}
	if !ok {
		fmt.Fprintln(cmd.OutOrStdout(), "Cancelled. Config unchanged.")
		return false, nil
	}
	return true, nil
}

// resolvedConfigPath returns the explicit --config path if given,
// otherwise falls back to config.DefaultPath. Returns "" if both
// fail (extremely rare; surface as internal error upstream).
func resolvedConfigPath() string {
	if flagConfigPath != "" {
		return flagConfigPath
	}
	path, err := config.DefaultPath()
	if err != nil {
		return ""
	}
	return path
}

// collectConfig builds a Config from flags, env, and (optionally) prompts.
func collectConfig(cmd *cobra.Command, f *initFlags, defaults *config.Config) (*config.Config, error) {
	out := *defaults

	stdin := bufio.NewReader(cmd.InOrStdin())
	stderr := cmd.ErrOrStderr()
	interactive := !f.noPrompt && isTerminal(os.Stdin)

	host, err := resolveString("host", f.host, "", out.Database.Host, interactive, stdin, stderr)
	if err != nil {
		return nil, err
	}
	out.Database.Host = host

	port, err := resolvePort(f.port, out.Database.Port, interactive, stdin, stderr)
	if err != nil {
		return nil, err
	}
	out.Database.Port = port

	database, err := resolveString("database", f.database, "", out.Database.Database, interactive, stdin, stderr)
	if err != nil {
		return nil, err
	}
	out.Database.Database = database

	user, err := resolveString("user", f.user, "", out.Database.User, interactive, stdin, stderr)
	if err != nil {
		return nil, err
	}
	out.Database.User = user

	password, err := resolvePassword(f, interactive, stderr)
	if err != nil {
		return nil, err
	}
	out.Database.Password = password

	sslmode, err := resolveString("sslmode", f.sslmode, "", out.Database.SSLMode, interactive, stdin, stderr)
	if err != nil {
		return nil, err
	}
	out.Database.SSLMode = sslmode

	return &out, nil
}

// resolveString picks a value for field from flag > env > default+prompt.
// It returns an error if the value ends up empty and prompting isn't
// available.
func resolveString(field, flagVal, envVal, defaultVal string, interactive bool, stdin *bufio.Reader, stderr io.Writer) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if envVal != "" {
		return envVal, nil
	}
	if !interactive {
		if defaultVal != "" {
			return defaultVal, nil
		}
		return "", fmt.Errorf("missing required field: %s", field)
	}
	return prompt(stdin, stderr, field, defaultVal)
}

// resolvePort handles the int-typed port field (defaults and validation
// differ from strings).
func resolvePort(flagVal, defaultVal int, interactive bool, stdin *bufio.Reader, stderr io.Writer) (int, error) {
	if flagVal != 0 {
		return flagVal, nil
	}
	if !interactive {
		if defaultVal != 0 {
			return defaultVal, nil
		}
		return 0, errors.New("missing required field: port")
	}
	raw, err := prompt(stdin, stderr, "port", strconv.Itoa(defaultVal))
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("port: invalid value %q", raw)
	}
	return n, nil
}

// resolvePassword fetches the password from flag, env, or prompt. It
// never returns the existing-config password as a silent default; we
// want the operator to either explicitly reuse it (via env) or re-enter
// it.
func resolvePassword(f *initFlags, interactive bool, stderr io.Writer) (string, error) {
	if f.password != "" {
		return f.password, nil
	}
	if f.passwordEnv != "" {
		v, ok := os.LookupEnv(f.passwordEnv)
		if !ok {
			return "", fmt.Errorf("env var %s is not set", f.passwordEnv)
		}
		return v, nil
	}
	if !interactive {
		return "", errors.New("missing required field: password (use --password, --password-env, or run interactively)")
	}
	return promptSecret(stderr, "password")
}

// shouldSaveAnyway asks the operator whether to save after a failed
// connection test. Only called in interactive mode.
func shouldSaveAnyway(cmd *cobra.Command) bool {
	if !isTerminal(os.Stdin) {
		return false
	}
	stdin := bufio.NewReader(cmd.InOrStdin())
	ans, err := prompt(stdin, cmd.ErrOrStderr(), "save config anyway? (y/N)", "N")
	if err != nil {
		return false
	}
	ans = strings.ToLower(strings.TrimSpace(ans))
	return ans == "y" || ans == "yes"
}

// prompt reads a line from stdin with a default in brackets. Returns
// the default if the user just hits enter. Writes the prompt to stderr
// (not stdout) so prompts don't get mixed into piped output.
func prompt(stdin *bufio.Reader, stderr io.Writer, label, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Fprintf(stderr, "%s [%s]: ", label, defaultVal)
	} else {
		fmt.Fprintf(stderr, "%s: ", label)
	}
	line, err := stdin.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return defaultVal, nil
	}
	return line, nil
}

// promptSecret reads a password from the terminal without echoing.
func promptSecret(stderr io.Writer, label string) (string, error) {
	fmt.Fprintf(stderr, "%s: ", label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	return string(b), nil
}

// isTerminal reports whether f is connected to a terminal.
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// printExtensionStatus writes one of the three extension-status
// messages described in the Stage 1 spec. Exact text is deliberately
// preserved — tests may assert on it.
func printExtensionStatus(w io.Writer, s pgstat.ExtensionStatus, database string) {
	fmt.Fprintln(w, "Checking pg_stat_statements...")
	switch {
	case s.InSharedPreloadLibraries && s.ExtensionInstalled:
		fmt.Fprintln(w, "✓ Extension is ready")
	case s.InSharedPreloadLibraries && !s.ExtensionInstalled:
		fmt.Fprintln(w, "✓ shared_preload_libraries includes pg_stat_statements")
		fmt.Fprintf(w, "✗ Extension is not created in database '%s'\n\n", database)
		fmt.Fprintln(w, "To enable query statistics, connect to '"+database+"' as a superuser and run:")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "    CREATE EXTENSION pg_stat_statements;")
	default:
		fmt.Fprintln(w, "✗ shared_preload_libraries does not include pg_stat_statements")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "This requires editing postgresql.conf and restarting PostgreSQL:")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "    # In postgresql.conf")
		fmt.Fprintln(w, "    shared_preload_libraries = 'pg_stat_statements'")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "    # Then restart")
		fmt.Fprintln(w, "    sudo systemctl restart postgresql")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "For managed databases (RDS, Cloud SQL, Neon, Supabase), enable the")
		fmt.Fprintln(w, "extension from your provider's dashboard. See:")
		fmt.Fprintln(w, "https://github.com/byksy/dbagent#enabling-pg_stat_statements")
	}
}

// printFinalMessage writes the closing block. When the extension is
// already ready, we skip the "once the extension is ready" line.
func printFinalMessage(w io.Writer, path string, ready bool) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Config saved to %s\n", path)
	fmt.Fprintln(w)
	if ready {
		fmt.Fprintln(w, "Run:")
		fmt.Fprintln(w, "    dbagent top")
	} else {
		fmt.Fprintln(w, "Once the extension is ready, run:")
		fmt.Fprintln(w, "    dbagent top")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Or re-run 'dbagent init --check' to verify.")
	}
}

