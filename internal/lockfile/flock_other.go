//go:build !unix

package lockfile

import (
	"errors"
	"os"
)

// The distribution targets are macOS/Linux (single-binary install via
// brew/curl). Non-unix platforms still compile but don't support locking;
// callers get an explicit error instead of silently losing mutual exclusion.
var errUnsupported = errors.New("lockfile: file locking is not supported on this platform")

func flockEx(f *os.File, nonblock bool) error { return errUnsupported }

func funlock(f *os.File) error { return errUnsupported }
