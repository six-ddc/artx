// Command artx is the CLI entry point for the agent-artifact vault and its
// human-review comment loop.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/six-ddc/artx/internal/vault"
)

// Exit code semantics (design doc §5): 0 success / 1 error / 2 not found.
const (
	ExitOK       = 0
	ExitError    = 1
	ExitNotFound = 2
)

func main() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "artx:", err)
		os.Exit(exitCodeFor(err))
	}
}

// exitCodeFor maps an error to an exit code.
func exitCodeFor(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, vault.ErrNotFound):
		return ExitNotFound
	default:
		return ExitError
	}
}
