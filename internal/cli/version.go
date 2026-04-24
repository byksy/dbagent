package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags. The Makefile populates
// it with `git describe --tags --always --dirty`, producing values
// like "v0.4.1" for a clean tagged build and
// "v0.4.1-7-gabcd123-dirty" for in-development work past a tag.
//
// Unset builds (plain `go build` with no ldflags, or `go install
// …@latest` off a non-tagged commit) see "dev" — this is the
// intentional fallback, not an error. Release installs that pin a
// tag pick up the Go module proxy's tag information automatically.
var Version = "dev"

// newVersionCmd builds the "version" subcommand.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "dbagent %s\n%s %s/%s\n",
				Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
