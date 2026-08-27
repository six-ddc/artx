package gitx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hasSystemGit(t *testing.T) {
	t.Helper()
	if !hasGit() {
		t.Skip("git not available on PATH")
	}
}

func TestNoRepoDegrades(t *testing.T) {
	dir := t.TempDir()
	r := Open(dir)
	if r.Available() {
		t.Fatal("Available() = true for a non-repo directory")
	}
	if _, err := r.HeadSHA(context.Background()); err == nil {
		t.Fatal("HeadSHA() on non-repo: want ErrNoRepo")
	}
}

func TestInitCommitAndLog(t *testing.T) {
	hasSystemGit(t)
	dir := t.TempDir()
	r := Open(dir)
	ctx := context.Background()

	if err := r.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !r.Available() {
		t.Fatal("Available() = false after Init")
	}

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := r.Commit(ctx, CommitOptions{Message: "first", Author: AuthorArtx})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if sha == "" {
		t.Fatal("Commit returned empty sha for a real change")
	}

	head, err := r.HeadSHA(ctx)
	if err != nil || head != sha {
		t.Fatalf("HeadSHA = %q, %v; want %q", head, err, sha)
	}

	rev, err := r.FileRev(ctx, "a.txt")
	if err != nil || rev != sha {
		t.Fatalf("FileRev = %q, %v; want %q", rev, err, sha)
	}

	// No-op commit: no changes staged, should return "" and no error.
	sha2, err := r.Commit(ctx, CommitOptions{Message: "noop", Author: AuthorArtx})
	if err != nil {
		t.Fatalf("Commit (noop): %v", err)
	}
	if sha2 != "" {
		t.Fatalf("Commit (noop) = %q, want empty", sha2)
	}

	content, err := r.ShowFile(ctx, sha, "a.txt")
	if err != nil {
		t.Fatalf("ShowFile: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("ShowFile = %q, want %q", content, "hello")
	}

	commits, err := r.LogFile(ctx, "a.txt", 10)
	if err != nil {
		t.Fatalf("LogFile: %v", err)
	}
	if len(commits) != 1 || commits[0].SHA != sha {
		t.Fatalf("LogFile = %+v, want single commit %q", commits, sha)
	}
}

func TestEnsureGitattributes(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureGitattributes(dir); err != nil {
		t.Fatalf("EnsureGitattributes: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if err != nil {
		t.Fatalf("read .gitattributes: %v", err)
	}
	if !strings.Contains(string(b), GitattributesLine) {
		t.Fatalf(".gitattributes = %q, missing rule line", b)
	}

	// Idempotent: existing content with other lines is preserved, and the
	// rule is not duplicated.
	if err := EnsureGitattributes(dir); err != nil {
		t.Fatalf("EnsureGitattributes (2nd call): %v", err)
	}
	b2, err := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b2), GitattributesLine) != 1 {
		t.Fatalf(".gitattributes rule duplicated: %q", b2)
	}
}

func TestEnsureGitattributesPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitattributes")
	if err := os.WriteFile(path, []byte("*.png binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGitattributes(dir); err != nil {
		t.Fatalf("EnsureGitattributes: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "*.png binary") || !strings.Contains(string(b), GitattributesLine) {
		t.Fatalf(".gitattributes = %q, want both lines preserved", b)
	}
}

func TestEnsureGitignore(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureGitignore(dir); err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(b), GitignoreLine) {
		t.Fatalf(".gitignore = %q, missing rule line", b)
	}

	// Idempotent: existing content with other lines is preserved, and the
	// rule is not duplicated.
	if err := EnsureGitignore(dir); err != nil {
		t.Fatalf("EnsureGitignore (2nd call): %v", err)
	}
	b2, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b2), GitignoreLine) != 1 {
		t.Fatalf(".gitignore rule duplicated: %q", b2)
	}
}

func TestEnsureGitignorePreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(path, []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGitignore(dir); err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "node_modules/") || !strings.Contains(string(b), GitignoreLine) {
		t.Fatalf(".gitignore = %q, want both lines preserved", b)
	}
}

// TestCommitWithoutGitIdentity: committing must still work even when the
// user hasn't configured global git config.
//
// user.useConfigOnly=true makes git refuse to guess an identity from
// username@hostname, equivalent to a CI runner environment hitting
// "unable to auto-detect email address". art creates and commits to its own
// repository and shouldn't depend on the user's global configuration.
func TestCommitWithoutGitIdentity(t *testing.T) {
	hasSystemGit(t)
	dir := t.TempDir()
	ctx := context.Background()
	r := Open(dir)

	if err := r.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := r.run(ctx, "config", "user.useConfigOnly", "true"); err != nil {
		t.Fatalf("config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sha, err := r.Commit(ctx, CommitOptions{Message: "artx: test", Author: AuthorArtx})
	if err != nil {
		t.Fatalf("Commit failed without a git identity configured: %v", err)
	}
	if sha == "" {
		t.Fatal("Commit succeeded but returned no sha")
	}

	out, err := r.run(ctx, "log", "-1", "--format=%an|%ae|%cn|%ce")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out); got != "artx|artx@localhost|artx|artx@localhost" {
		t.Errorf("author/committer identity = %q, want artx|artx@localhost|artx|artx@localhost", got)
	}
}
