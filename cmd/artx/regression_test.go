package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/client"
	"github.com/six-ddc/artx/internal/vault"
)

// newTestVault creates a fresh, isolated vault (own global registry) for CLI
// integration tests and returns its root path.
func newTestVault(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ARTX_VAULT", "")
	dir := t.TempDir()
	if _, err := vault.Init(context.Background(), dir, "test"); err != nil {
		t.Fatal(err)
	}
	return dir
}

// captureStdout runs fn with os.Stdout redirected, so CLI commands that
// print/emit directly to os.Stdout (rather than cmd.OutOrStdout()) don't
// clutter `go test -v` output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	data, _ := io.ReadAll(r)
	return string(data)
}

// TestDoctorExitCodeReflectsUnresolvedIssues is the MINOR-3 regression test:
// `artx doctor` must exit non-zero when it finds an issue it did not (or
// could not) fix, and exit zero once the vault is clean.
func TestDoctorExitCodeReflectsUnresolvedIssues(t *testing.T) {
	dir := newTestVault(t)

	// An artifact with no discoverable aid is an unresolvable "missing-aid"
	// issue in the current doctor implementation (no auto-fix path for it).
	if err := os.MkdirAll(filepath.Join(dir, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken", "index.md"), []byte("# no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var err error
	captureStdout(t, func() {
		root := NewRootCmd()
		root.SetArgs([]string{"--vault", dir, "--json", "doctor"})
		err = root.Execute()
	})
	if err == nil {
		t.Fatal("doctor with an unresolved issue returned nil error, want non-nil (exit 1)")
	}

	// A clean vault (remove the broken artifact) exits 0.
	if err := os.RemoveAll(filepath.Join(dir, "broken")); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		root := NewRootCmd()
		root.SetArgs([]string{"--vault", dir, "--json", "doctor"})
		err = root.Execute()
	})
	if err != nil {
		t.Fatalf("doctor on a clean vault returned an error, want nil: %v", err)
	}

	// A fixable issue (missing .gitattributes rule) must exit 1 without
	// --fix, and 0 once --fix resolves it.
	gaPath := filepath.Join(dir, ".gitattributes")
	if err := os.WriteFile(gaPath, []byte("*.png binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		root := NewRootCmd()
		root.SetArgs([]string{"--vault", dir, "--json", "doctor"})
		err = root.Execute()
	})
	if err == nil {
		t.Fatal("doctor with a missing gitattributes rule (no --fix) returned nil, want non-nil (exit 1)")
	}
	captureStdout(t, func() {
		root := NewRootCmd()
		root.SetArgs([]string{"--vault", dir, "--json", "doctor", "--fix"})
		err = root.Execute()
	})
	if err != nil {
		t.Fatalf("doctor --fix on a fixable issue returned an error, want nil: %v", err)
	}
}

// TestMapNotFoundCollapsesToSingleLayer is the MINOR-6 regression test:
// mapNotFound must not stack the serve-side "thread not found" message on
// top of vault.ErrNotFound's own message, and must still satisfy
// errors.Is(err, vault.ErrNotFound) so exit code 2 is preserved.
func TestMapNotFoundCollapsesToSingleLayer(t *testing.T) {
	original := &client.Error{
		Status:   http.StatusNotFound,
		Response: api.ErrorResponse{Error: api.ErrNotFound, Message: "thread not found"},
	}

	mapped := mapNotFound(original)

	if !errors.Is(mapped, vault.ErrNotFound) {
		t.Fatalf("mapNotFound(%v) = %v, want errors.Is match against vault.ErrNotFound", original, mapped)
	}
	if mapped.Error() != vault.ErrNotFound.Error() {
		t.Fatalf("mapNotFound message = %q, want the clean single-layer %q (not doubled with the client message)",
			mapped.Error(), vault.ErrNotFound.Error())
	}
	if strings.Contains(mapped.Error(), "thread not found") {
		t.Fatalf("mapNotFound message still carries the redundant client message: %q", mapped.Error())
	}

	// A non-not_found client error must pass through unchanged.
	other := &client.Error{Status: http.StatusInternalServerError, Response: api.ErrorResponse{Error: api.ErrInternal, Message: "boom"}}
	if got := mapNotFound(other); got != other {
		t.Fatalf("mapNotFound(non-not_found error) = %v, want unchanged", got)
	}

	// nil passes through as nil.
	if got := mapNotFound(nil); got != nil {
		t.Fatalf("mapNotFound(nil) = %v, want nil", got)
	}
}

// TestResolveMissingThreadIsSingleLayerAndExitsNotFound exercises the same
// fix end-to-end on the local (no-serve) path, where vault.FindThread
// already returned a clean single-layer error — this pins that behavior
// stays that way now that the client path matches it.
func TestResolveMissingThreadIsSingleLayerAndExitsNotFound(t *testing.T) {
	dir := newTestVault(t)

	var err error
	captureStdout(t, func() {
		root := NewRootCmd()
		root.SetArgs([]string{"--vault", dir, "resolve", "cnothing"})
		err = root.Execute()
	})
	if err == nil {
		t.Fatal("resolve on a nonexistent thread should error")
	}
	if !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("error = %v, want errors.Is(err, vault.ErrNotFound)", err)
	}
	if strings.Count(err.Error(), "not found") > 1 {
		t.Fatalf("error message has redundant layering: %q", err.Error())
	}
}
