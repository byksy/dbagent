package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/byksy/dbagent/internal/cli/style"
	"github.com/byksy/dbagent/internal/config"
	"github.com/spf13/cobra"
)

// newConfigCmd builds the `dbagent config` parent and its three
// subcommands. All of them honour the persistent --config flag for
// path overrides; defaults follow the same XDG rules Stage 1 wired
// up for every other command.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage dbagent configuration",
		Long: `View, delete, or reset your dbagent configuration.

The config file lives at $XDG_CONFIG_HOME/dbagent/config.yaml (usually
~/.config/dbagent/config.yaml). Use 'dbagent config path' to see the
exact location on your system.

To create a new config interactively, use 'dbagent init'.
To overwrite an existing config non-interactively, use 'dbagent init
--force' with flags.`,
	}
	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigPathCmd())
	cmd.AddCommand(newConfigResetCmd())
	return cmd
}

// newConfigShowCmd prints the current config with the password
// redacted. Output is plain-text YAML so it stays pipeable; only the
// trailing "# Config file: …" footer uses the muted style.
func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the current config with the password redacted",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigShow(cmd)
		},
	}
}

func runConfigShow(cmd *cobra.Command) error {
	path := resolvedConfigPath()
	if path == "" {
		return newExitError(ExitInternal, errors.New("cannot resolve default config path"))
	}

	cfg, err := config.Load(path)
	if err != nil {
		if errors.Is(err, config.ErrConfigNotFound) {
			return newExitError(ExitNoConfig,
				fmt.Errorf("no config found at %s; run 'dbagent init' first", path))
		}
		return newExitError(ExitUsageError, err)
	}

	w := cmd.OutOrStdout()
	w.Write(config.Marshal(cfg.Redacted()))
	fmt.Fprintln(w)
	fmt.Fprintln(w, style.StyleMuted.Render("# Config file: "+path))
	return nil
}

// newConfigPathCmd prints the resolved config path. Stdout is
// intentionally one line so `cat $(dbagent config path)` works; any
// "file does not exist" note goes to stderr so pipes stay clean.
func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the resolved config file path",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigPath(cmd)
		},
	}
}

func runConfigPath(cmd *cobra.Command) error {
	path := resolvedConfigPath()
	if path == "" {
		return newExitError(ExitInternal, errors.New("cannot resolve default config path"))
	}

	fmt.Fprintln(cmd.OutOrStdout(), path)

	exists, err := config.ConfigExists(path)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "(%s)\n", err)
		return nil
	}
	if !exists {
		fmt.Fprintln(cmd.ErrOrStderr(), "(file does not exist; run 'dbagent init' to create it)")
	}
	return nil
}

// configResetFlags captures the per-invocation options for
// `dbagent config reset`.
type configResetFlags struct {
	force bool
}

// newConfigResetCmd deletes the config file after confirmation.
// Missing files are treated as success (idempotent). Non-interactive
// use without --force fails loudly rather than deleting silently.
func newConfigResetCmd() *cobra.Command {
	f := &configResetFlags{}
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Delete the config file (prompts for confirmation)",
		Long: `Delete the current config file after prompting for confirmation.

Removing a non-existent config is a no-op (exit 0). Pass --force to
skip the confirmation prompt — required when stdin is not a
terminal (CI, piped input).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigReset(cmd, f)
		},
	}
	cmd.Flags().BoolVar(&f.force, "force", false, "skip the confirmation prompt")
	return cmd
}

func runConfigReset(cmd *cobra.Command, f *configResetFlags) error {
	path := resolvedConfigPath()
	if path == "" {
		return newExitError(ExitInternal, errors.New("cannot resolve default config path"))
	}

	exists, err := config.ConfigExists(path)
	if err != nil {
		return newExitError(ExitUsageError, err)
	}
	if !exists {
		fmt.Fprintf(cmd.ErrOrStderr(), "no config to reset at %s\n", path)
		return nil
	}

	if !f.force {
		if !isTerminal(os.Stdin) {
			return newExitError(ExitUsageError,
				errors.New("cannot prompt for confirmation in non-interactive mode; use --force to bypass"))
		}
		if err := showConfigForReset(cmd, path); err != nil {
			return err
		}
		ok, err := confirm(cmd.InOrStdin(), cmd.ErrOrStderr(), "Delete this config? (y/N):")
		if err != nil {
			return newExitError(ExitInternal, err)
		}
		if !ok {
			fmt.Fprintln(cmd.OutOrStdout(), "Cancelled. Config unchanged.")
			return nil
		}
	}

	if err := config.DeleteConfig(path); err != nil {
		return newExitError(ExitInternal, err)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s Config deleted: %s\n",
		style.StyleSuccess.Render("✓"), path)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run 'dbagent init' to create a new one.")
	return nil
}

// showConfigForReset prints the redacted config so the user sees
// what's about to be removed. Load failures fall back to a minimal
// notice rather than aborting the reset — a corrupt config is
// precisely what reset is for.
func showConfigForReset(cmd *cobra.Command, path string) error {
	w := cmd.OutOrStdout()
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(w, "(could not parse current config: %v)\n", err)
		fmt.Fprintln(w)
		return nil
	}
	w.Write(config.Marshal(cfg.Redacted()))
	fmt.Fprintln(w)
	fmt.Fprintln(w, style.StyleMuted.Render("# Config file: "+path))
	fmt.Fprintln(w)
	return nil
}

