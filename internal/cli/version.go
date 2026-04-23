package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// version is the hardcoded version string for Stage 1. A real release
// will substitute this at build time.
const version = "v0.1.0-dev"

// newVersionCmd builds the "version" subcommand.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "dbagent %s\n%s %s/%s\n",
				version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
