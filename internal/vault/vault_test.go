package vault

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/config"
	"github.com/six-ddc/artx/internal/gitx"
)

// newGitRepo creates a plain (non-vault) git repository in a fresh temp dir,
// skipping the test when the system has no git.
func newGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := gitx.Open(dir).Init(context.Background()); err != nil {
		t.Skip("system git not available")
	}
	return dir
}

func isolateRegistry(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ARTX_VAULT", "")
}

func TestInitOpenIdempotent(t *testing.T) {
	isolateRegistry(t)
	dir := t.TempDir()
	ctx := context.Background()

	v1, err := Init(ctx, dir, InitOptions{Name: "work"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if v1.Name != "work" {
		t.Errorf("Name = %q, want work", v1.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, AgentsFile)); err != nil {
		t.Errorf("AGENTS.md not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitattributes")); err != nil {
		t.Errorf(".gitattributes not created: %v", err)
	}

	// Idempotent: a second Init on the same dir must not error and must
	// not clobber an AGENTS.md the user has since edited.
	agentsPath := filepath.Join(dir, AgentsFile)
	if err := os.WriteFile(agentsPath, []byte("custom content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(ctx, dir, InitOptions{Name: "work"}); err != nil {
		t.Fatalf("Init (2nd): %v", err)
	}
	b, err := os.ReadFile(agentsPath)
	if err != nil || string(b) != "custom content" {
		t.Errorf("AGENTS.md was overwritten on re-Init: %q, %v", b, err)
	}

	v2, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if v2.Root != v1.Root {
		t.Errorf("Root mismatch: %q vs %q", v2.Root, v1.Root)
	}
}

func TestOpenNonVaultFails(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir, ""); err == nil {
		t.Fatal("Open on a non-vault directory succeeded, want an error")
	}
}

func TestInitRefusesInsideRepo(t *testing.T) {
	isolateRegistry(t)
	repo := newGitRepo(t)
	ctx := context.Background()

	// A nested, not-yet-existing subdirectory of someone else's repo.
	target := filepath.Join(repo, "sub", "vault")
	if _, err := Init(ctx, target, InitOptions{}); !errors.Is(err, ErrInsideRepo) {
		t.Fatalf("Init inside a repo: err = %v, want ErrInsideRepo", err)
	}
	// The refusal must leave no trace behind.
	if _, err := os.Stat(filepath.Join(repo, "sub")); !os.IsNotExist(err) {
		t.Errorf("refused Init left %s behind", filepath.Join(repo, "sub"))
	}

	// The repository root itself (the incident shape: a project repo with
	// history) must be refused too.
	if _, err := gitx.Open(repo).Commit(ctx, gitx.CommitOptions{Message: "seed", AllowEmpty: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(ctx, repo, InitOptions{}); !errors.Is(err, ErrInsideRepo) {
		t.Fatalf("Init at a repo root: err = %v, want ErrInsideRepo", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ArtDir)); !os.IsNotExist(err) {
		t.Errorf("refused Init left %s behind", ArtDir)
	}
}

func TestInitRefusesNonEmpty(t *testing.T) {
	isolateRegistry(t)
	ctx := context.Background()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(ctx, dir, InitOptions{}); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("Init in a non-empty dir: err = %v, want ErrNotEmpty", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ArtDir)); !os.IsNotExist(err) {
		t.Errorf("refused Init left %s behind", ArtDir)
	}

	// Finder droppings don't count as data.
	dsOnly := t.TempDir()
	if err := os.WriteFile(filepath.Join(dsOnly, ".DS_Store"), []byte{0}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(ctx, dsOnly, InitOptions{}); err != nil {
		t.Fatalf("Init with only .DS_Store present: %v", err)
	}

	// Force overrides.
	if _, err := Init(ctx, dir, InitOptions{Force: true}); err != nil {
		t.Fatalf("Init with Force in a non-empty dir: %v", err)
	}
}

func TestInitForceInsideRepo(t *testing.T) {
	isolateRegistry(t)
	repo := newGitRepo(t)
	target := filepath.Join(repo, "vault")

	v, err := Init(context.Background(), target, InitOptions{Force: true})
	if err != nil {
		t.Fatalf("Init with Force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(v.Root, config.VaultConfigPath)); err != nil {
		t.Errorf("config.yaml not created: %v", err)
	}
}

func TestNewScanLookup(t *testing.T) {
	isolateRegistry(t)
	dir := t.TempDir()
	ctx := context.Background()
	v, err := Init(ctx, dir, InitOptions{Name: "work"})
	if err != nil {
		t.Fatal(err)
	}

	art, err := v.New("payment-refactor", api.DocTypeMD, "Payment Refactor")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if art.ID == "" {
		t.Fatal("New did not assign an id")
	}
	if _, err := os.Stat(art.Path); err != nil {
		t.Fatalf("skeleton file not written: %v", err)
	}

	// Duplicate slug must fail.
	if _, err := v.New("payment-refactor", api.DocTypeMD, ""); err != ErrExists {
		t.Fatalf("New (dup slug) = %v, want ErrExists", err)
	}

	htmlArt, err := v.New("pricing-demo", api.DocTypeHTML, "Pricing Demo")
	if err != nil {
		t.Fatalf("New (html): %v", err)
	}

	arts, err := v.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(arts) != 2 {
		t.Fatalf("Scan() = %d artifacts, want 2", len(arts))
	}

	byLookup, err := v.Lookup("payment-refactor")
	if err != nil || byLookup.ID != art.ID {
		t.Fatalf("Lookup(slug) = %+v, %v; want id %q", byLookup, err, art.ID)
	}
	byID, err := v.Lookup(art.ID)
	if err != nil || byID.Slug != "payment-refactor" {
		t.Fatalf("Lookup(id) = %+v, %v", byID, err)
	}
	byPrefix, err := v.Lookup(art.ID[:3])
	if err != nil || byPrefix.ID != art.ID {
		t.Fatalf("Lookup(prefix) = %+v, %v; want id %q", byPrefix, err, art.ID)
	}
	if _, err := v.Lookup("does-not-exist"); err != ErrNotFound {
		t.Fatalf("Lookup(missing) = %v, want ErrNotFound", err)
	}

	if htmlArt.Type != api.DocTypeHTML {
		t.Fatalf("html artifact type = %q", htmlArt.Type)
	}
}

func TestDocsAndThreadsRoundTrip(t *testing.T) {
	isolateRegistry(t)
	dir := t.TempDir()
	ctx := context.Background()
	v, err := Init(ctx, dir, InitOptions{Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	art, err := v.New("doc-a", api.DocTypeMD, "Doc A")
	if err != nil {
		t.Fatal(err)
	}

	docs, err := v.Docs(ctx, "http://127.0.0.1:7777")
	if err != nil {
		t.Fatalf("Docs: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != art.ID {
		t.Fatalf("Docs() = %+v, want one doc with id %q", docs, art.ID)
	}
	if docs[0].URL == "" {
		t.Error("Doc.URL not filled with baseURL set")
	}

	comments, err := v.Threads(ctx, art)
	if err != nil {
		t.Fatalf("Threads: %v", err)
	}
	if len(comments.Threads) != 0 {
		t.Fatalf("Threads() = %+v, want empty for a fresh doc", comments.Threads)
	}

	all, err := v.AllThreads(ctx, "")
	if err != nil {
		t.Fatalf("AllThreads: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("AllThreads() = %+v, want empty", all)
	}
}

// TestAllThreadsAndScanMarshalToEmptyArrayNotNull is a BLOCKER-1 regression
// test at the vault layer: artx comments --json (no --doc) marshals
// AllThreads' return value directly as a bare JSON array. A nil slice for a
// vault with zero comments would emit `null` instead of `[]`.
func TestAllThreadsAndScanMarshalToEmptyArrayNotNull(t *testing.T) {
	isolateRegistry(t)
	dir := t.TempDir()
	ctx := context.Background()
	v, err := Init(ctx, dir, InitOptions{Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	// A vault with no artifacts at all: Scan() and AllThreads() must both
	// return non-nil, empty-marshaling slices.
	arts, err := v.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if arts == nil {
		t.Fatal("Scan() returned nil, want non-nil empty slice")
	}

	threads, err := v.AllThreads(ctx, "")
	if err != nil {
		t.Fatalf("AllThreads: %v", err)
	}
	if threads == nil {
		t.Fatal("AllThreads() returned nil, want non-nil empty slice")
	}
	data, err := json.Marshal(threads)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "[]" {
		t.Fatalf("json.Marshal(AllThreads()) = %s, want []", data)
	}

	// Also with a doc present but zero comments on it.
	if _, err := v.New("doc-a", api.DocTypeMD, "Doc A"); err != nil {
		t.Fatal(err)
	}
	threads2, err := v.AllThreads(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	data2, err := json.Marshal(threads2)
	if err != nil {
		t.Fatal(err)
	}
	if string(data2) != "[]" {
		t.Fatalf("json.Marshal(AllThreads()) with a commentless doc = %s, want []", data2)
	}
}

func TestAuthorPriority(t *testing.T) {
	v := &Vault{}
	t.Setenv("ARTX_AUTHOR", "")
	t.Setenv("USER", "sysuser")
	if got := v.Author(); got != "sysuser" {
		t.Errorf("Author() = %q, want $USER fallback %q", got, "sysuser")
	}

	t.Setenv("ARTX_AUTHOR", "envuser")
	if got := v.Author(); got != "envuser" {
		t.Errorf("Author() = %q, want $ARTX_AUTHOR %q", got, "envuser")
	}

	v.Cfg = &config.Vault{Author: "cfguser"}
	if got := v.Author(); got != "cfguser" {
		t.Errorf("Author() = %q, want vault config Author %q", got, "cfguser")
	}
}

// ---------------------------------------------------------------------------
// ResolvePath: the required acceptance test.
// ---------------------------------------------------------------------------

func TestResolvePathRejectsDotDotTraversal(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "vault")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// A sibling secret outside the vault root.
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := &Vault{Root: root}

	if _, err := v.ResolvePath("../secret.txt"); err != ErrOutsideVault {
		t.Fatalf("ResolvePath(../secret.txt) = %v, want ErrOutsideVault", err)
	}
	if _, err := v.ResolvePath("a/../../secret.txt"); err != ErrOutsideVault {
		t.Fatalf("ResolvePath(a/../../secret.txt) = %v, want ErrOutsideVault", err)
	}
	// An absolute-looking rel is still just joined under root (filepath.Join
	// semantics), so it stays safely contained rather than escaping.
	resolvedAbs, err := v.ResolvePath("/etc/passwd")
	if err != nil {
		t.Fatalf("ResolvePath(/etc/passwd) = %v, want no error (contained under root)", err)
	}
	evalRoot, _ := filepath.EvalSymlinks(root)
	if !pathWithin(evalRoot, resolvedAbs) {
		t.Fatalf("ResolvePath(/etc/passwd) = %q, escapes root %q", resolvedAbs, evalRoot)
	}

	// A legitimate in-vault path must resolve fine.
	if err := os.MkdirAll(filepath.Join(root, "doc-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	inVaultFile := filepath.Join(root, "doc-a", "index.md")
	if err := os.WriteFile(inVaultFile, []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := v.ResolvePath("doc-a/index.md")
	if err != nil {
		t.Fatalf("ResolvePath(doc-a/index.md): %v", err)
	}
	resolvedEval, _ := filepath.EvalSymlinks(resolved)
	wantEval, _ := filepath.EvalSymlinks(inVaultFile)
	if resolvedEval != wantEval {
		t.Fatalf("ResolvePath(doc-a/index.md) = %q, want %q", resolved, inVaultFile)
	}
}

func TestResolvePathRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "vault")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A symlink planted inside the vault that points outside it.
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks not supported in this environment: %v", err)
	}

	v := &Vault{Root: root}
	if _, err := v.ResolvePath("escape/secret.txt"); err != ErrOutsideVault {
		t.Fatalf("ResolvePath(escape/secret.txt) = %v, want ErrOutsideVault", err)
	}
}

// TestNewCommitsSkeleton covers the precondition for "document id is
// recoverable": once artx new writes the skeleton, there must immediately be
// a commit, so git always holds a version carrying the aid that the watcher
// can recover after an agent's wholesale rewrite wipes it out.
func TestNewCommitsSkeleton(t *testing.T) {
	isolateRegistry(t)
	dir := t.TempDir()
	ctx := context.Background()

	v, err := Init(ctx, dir, InitOptions{Name: "work"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if v.Git == nil || !v.Git.Available() {
		t.Skip("no usable git in this environment")
	}

	for _, typ := range []string{api.DocTypeMD, api.DocTypeHTML} {
		slug := "doc-" + typ
		a, err := v.New(slug, typ, "Title")
		if err != nil {
			t.Fatalf("New(%s): %v", typ, err)
		}

		committed, err := v.Git.ShowFile(ctx, "HEAD", a.RelPath)
		if err != nil {
			t.Fatalf("%s skeleton was not committed: %v", typ, err)
		}
		if !strings.Contains(string(committed), a.ID) {
			t.Errorf("%s skeleton committed to git does not contain aid %q:\n%s", typ, a.ID, committed)
		}

		logs, err := v.Git.LogFile(ctx, a.RelPath, 10)
		if err != nil || len(logs) == 0 {
			t.Fatalf("%s git log is empty: %v", typ, err)
		}
		if want := "artx: new " + slug; logs[len(logs)-1].Subject != want {
			t.Errorf("%s first commit subject = %q, want %q", typ, logs[len(logs)-1].Subject, want)
		}
	}
}

// TestNewWithoutGitStillWorks: creation still succeeds without git; it just doesn't commit.
func TestNewWithoutGitStillWorks(t *testing.T) {
	isolateRegistry(t)
	root := t.TempDir()
	v := &Vault{Root: root} // Git is nil
	a, err := v.New("plain", api.DocTypeMD, "Title")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(a.Path); err != nil {
		t.Fatalf("file was not created: %v", err)
	}
}

// TestInitCommitsSkeleton: init must commit .gitattributes / AGENTS.md /
// config.yaml itself. The watcher's auto-commit only covers the artifact
// directories plus .artx/comments — nothing else manages these root-level
// files, and without the merge=union setting in .gitattributes landing in
// the repo, multi-machine convergence never happens.
func TestInitCommitsSkeleton(t *testing.T) {
	isolateRegistry(t)
	dir := t.TempDir()
	ctx := context.Background()

	v, err := Init(ctx, dir, InitOptions{Name: "work"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if v.Git == nil || !v.Git.Available() {
		t.Skip("no usable git in this environment")
	}

	for _, rel := range []string{".gitattributes", ".gitignore", AgentsFile, config.VaultConfigPath} {
		b, err := v.Git.ShowFile(ctx, "HEAD", rel)
		if err != nil {
			t.Errorf("%s was not committed by init: %v", rel, err)
			continue
		}
		if len(b) == 0 {
			t.Errorf("%s was committed with empty content", rel)
		}
	}
	if b, err := v.Git.ShowFile(ctx, "HEAD", ".gitattributes"); err == nil {
		if !strings.Contains(string(b), "union") {
			t.Errorf(".gitattributes is missing merge=union:\n%s", b)
		}
	}
	if b, err := v.Git.ShowFile(ctx, "HEAD", ".gitignore"); err == nil {
		if !strings.Contains(string(b), gitx.GitignoreLine) {
			t.Errorf(".gitignore committed at HEAD is missing %q:\n%s", gitx.GitignoreLine, b)
		}
	}
}

// TestInitGitignoreCoversServeLock is the required regression test for the
// serve.lock leak: --token mode writes the bearer token in plaintext into
// .artx/serve.lock (0600 only keeps other local users out, it does nothing
// to stop a stray `git add -A` from committing it), so `artx init` must seed
// a .gitignore that excludes it, and re-running Init must not duplicate the
// rule or clobber user-added lines.
func TestInitGitignoreCoversServeLock(t *testing.T) {
	isolateRegistry(t)
	dir := t.TempDir()
	ctx := context.Background()

	v, err := Init(ctx, dir, InitOptions{Name: "work"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	gitignorePath := filepath.Join(dir, ".gitignore")
	b, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf(".gitignore was not created by Init: %v", err)
	}
	if !strings.Contains(string(b), ".artx/serve.lock") {
		t.Fatalf(".gitignore = %q, want it to exclude .artx/serve.lock", b)
	}

	// A user's own .gitignore content must survive re-Init, and the rule
	// must not be duplicated.
	if err := os.WriteFile(gitignorePath, append(b, []byte("*.log\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(ctx, dir, InitOptions{Name: "work"}); err != nil {
		t.Fatalf("Init (2nd): %v", err)
	}
	b2, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b2), "*.log") {
		t.Fatalf(".gitignore = %q, want the user-added line preserved", b2)
	}
	if strings.Count(string(b2), gitx.GitignoreLine) != 1 {
		t.Fatalf(".gitignore = %q, want exactly one occurrence of %q", b2, gitx.GitignoreLine)
	}

	if v.Git == nil || !v.Git.Available() {
		t.Skip("no usable git in this environment; skipping HEAD content check")
	}
	committed, err := v.Git.ShowFile(ctx, "HEAD", ".gitignore")
	if err != nil {
		t.Fatalf(".gitignore was not committed by init: %v", err)
	}
	if !strings.Contains(string(committed), gitx.GitignoreLine) {
		t.Fatalf(".gitignore committed at HEAD = %q, missing %q", committed, gitx.GitignoreLine)
	}
}
