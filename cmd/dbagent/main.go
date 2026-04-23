// Command dbagent is a PostgreSQL query analyzer CLI. All real work
// happens in internal/cli; main only runs the root command and
// translates a typed *cli.ExitError into the corresponding exit code.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/byksy/dbagent/internal/cli"
)

func main() {
	err := cli.Execute()
	if err == nil {
		return
	}
	// Print the error to stderr. Cobra is configured with SilenceErrors,
	// so we own the final rendering.
	fmt.Fprintf(os.Stderr, "dbagent: %s\n", err.Error())

	var ec *cli.ExitError
	if errors.As(err, &ec) {
		os.Exit(ec.Code)
	}
	os.Exit(1)
}
