// Package version holds version information injected at build time.
package version

// Version is injected via ldflags: -X github.com/six-ddc/artx/internal/version.Version=v0.1.0
var Version = "dev"

// Commit is injected via ldflags.
var Commit = "none"

// Date is injected via ldflags: the release build's timestamp (RFC3339).
var Date = "unknown"

// String returns the full version string, e.g. "v0.1.0 (abc1234, 2026-08-27T10:00:00Z)".
func String() string {
	return Version + " (" + Commit + ", " + Date + ")"
}
