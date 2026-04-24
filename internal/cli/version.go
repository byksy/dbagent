package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags. The Makefile populates
// it with `git describe --tags --always --dirty`, producing values
// like "v0.4.1" for a clean tagged build and
// "v0.4.1-7-gabcd123-dirty" for in-development work past a tag.
//
// When it is left at its "dev" default (plain `go build`, or
// `go install …@vX.Y.Z` which ignores ldflags), resolvedVersion()
// falls back to runtime build info so installs from a pinned tag
// still show that tag instead of "dev". Builds off main without a
// tag will continue to show "dev" with a pseudo-version appended.
var Version = "dev"

// resolvedVersion returns the version string to print. Prefers the
// ldflags value when it's been set; otherwise reads the Go module's
// build-info main version, which `go install github.com/.../@vX.Y.Z`
// sets automatically.
func resolvedVersion() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return Version
}

// newVersionCmd builds the "version" subcommand.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "dbagent %s\n%s %s/%s\n",
				resolvedVersion(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
