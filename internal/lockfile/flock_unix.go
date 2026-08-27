//go:build unix

package lockfile

import (
	"os"
	"syscall"
)

// flockEx places an exclusive advisory lock on f. With nonblock=true it
// returns ErrLocked immediately if the lock is unavailable.
//
// We use syscall.Flock rather than a third-party library: BSD flock
// semantics are consistent across macOS/Linux, and the kernel releases the
// lock automatically on process exit (including kill / panic) — exactly the
// property the serve-detection protocol depends on. A third-party library
// wouldn't improve on this, and would just add a dependency.
func flockEx(f *os.File, nonblock bool) error {
	how := syscall.LOCK_EX
	if nonblock {
		how |= syscall.LOCK_NB
	}
	err := syscall.Flock(int(f.Fd()), how)
	if err == syscall.EWOULDBLOCK {
		return ErrLocked
	}
	return err
}

// funlock releases the advisory lock.
func funlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
