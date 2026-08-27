// Package gitx is a thin wrapper around the system git binary (via exec, not
// go-git).
//
// Owner: W-core.
//
// Every method returns ErrNoRepo when the repository isn't initialized or
// git isn't available, and callers must always **degrade gracefully rather
// than error out**: art's core functionality does not depend on git — git
// only provides versioning and sync.
package gitx

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrNoRepo indicates the directory isn't a git repository, or the system
// has no git.
var ErrNoRepo = errors.New("gitx: not a git repository")

// Repo is bound to a working-tree root directory.
type Repo struct {
	root string
}

// Open returns a Repo bound to root, without validating it.
func Open(root string) *Repo { return &Repo{root: root} }

func hasGit() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Available reports whether the system git is available and root is a
// repository.
func (r *Repo) Available() bool {
	if !hasGit() {
		return false
	}
	out, err := r.run(context.Background(), "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "true"
}

// Toplevel returns the root of the git worktree containing dir, or "" when
// dir is not inside one (or git is unavailable) — errors degrade to "" per
// the package's git-is-optional philosophy. A package function rather than a
// Repo method because callers may probe ancestors of a directory that does
// not exist yet.
func Toplevel(ctx context.Context, dir string) string {
	if !hasGit() {
		return ""
	}
	out, err := Open(dir).run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// committerEnv supplies a committer identity for every git invocation.
//
// art creates and commits to its own repository, and shouldn't require the
// user to have global git config set up first. Without one, git tries to
// guess an identity from username@hostname, and refuses to commit outright
// when it can't guess a valid domain (the classic case being "unable to
// auto-detect email address ... (none)" on a CI runner) — and since Commit's
// errors are swallowed inside the watcher, the visible symptom is just
// "auto-commit silently doesn't work". The author identity is already set
// explicitly via Commit's --author, so here we only need to pin down the
// committer.
var committerEnv = []string{
	"GIT_COMMITTER_NAME=artx",
	"GIT_COMMITTER_EMAIL=artx@localhost",
}

func (r *Repo) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(), committerEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

// Init runs git init (idempotent).
func (r *Repo) Init(ctx context.Context) error {
	if !hasGit() {
		return ErrNoRepo
	}
	_, err := r.run(ctx, "init")
	if err != nil {
		return ErrNoRepo
	}
	return nil
}

// HeadSHA returns HEAD's short sha; if there are no commits yet, it returns
// an empty string and nil.
func (r *Repo) HeadSHA(ctx context.Context) (string, error) {
	if !r.Available() {
		return "", ErrNoRepo
	}
	out, err := r.run(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		// The repo exists but has no commits yet.
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// FileRev returns the short sha of the commit that last changed relPath; an
// empty string if there's no history for it.
func (r *Repo) FileRev(ctx context.Context, relPath string) (string, error) {
	if !r.Available() {
		return "", ErrNoRepo
	}
	out, err := r.run(ctx, "log", "-1", "--format=%h", "--", relPath)
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// ShowFile returns the content of relPath at rev, used for viewing historical
// versions via ?v=sha, and as the source of the "previous version" when the
// watcher computes a diff.
func (r *Repo) ShowFile(ctx context.Context, rev, relPath string) ([]byte, error) {
	if !r.Available() {
		return nil, ErrNoRepo
	}
	relPath = filepath.ToSlash(relPath)
	cmd := exec.CommandContext(ctx, "git", "show", rev+":"+relPath)
	cmd.Dir = r.root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

// Author distinguishes three kinds of identity written into a commit's
// author field.
type Author string

const (
	AuthorAgent Author = "agent" // content written by an agent
	AuthorHuman Author = "human" // edited directly in the browser (M2)
	AuthorArtx  Author = "artx"  // artx itself: aid injection, compact
)

// CommitOptions controls a single commit.
type CommitOptions struct {
	Message    string
	Author     Author
	Paths      []string // empty means git add -A
	AllowEmpty bool
}

// Commit runs add + commit and returns the new commit's short sha.
// When there are no changes, it returns an empty string and nil (not an
// error).
func (r *Repo) Commit(ctx context.Context, opts CommitOptions) (string, error) {
	if !r.Available() {
		return "", ErrNoRepo
	}
	if len(opts.Paths) == 0 {
		if _, err := r.run(ctx, "add", "-A"); err != nil {
			return "", err
		}
	} else {
		args := append([]string{"add", "--"}, opts.Paths...)
		if _, err := r.run(ctx, args...); err != nil {
			return "", err
		}
	}

	if !opts.AllowEmpty {
		out, err := r.run(ctx, "status", "--porcelain")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(out) == "" {
			return "", nil
		}
	}

	authorName := "artx"
	switch opts.Author {
	case AuthorAgent:
		authorName = "artx-agent"
	case AuthorHuman:
		authorName = "artx-human"
	case AuthorArtx:
		authorName = "artx"
	}
	authorEnv := authorName + " <" + authorName + "@localhost>"

	args := []string{"commit", "-m", opts.Message, "--author=" + authorEnv}
	if opts.AllowEmpty {
		args = append(args, "--allow-empty")
	}
	if _, err := r.run(ctx, args...); err != nil {
		return "", err
	}
	return r.HeadSHA(ctx)
}

// LogFile returns relPath's commit history (the most recent n entries), for
// use by the ?v=sha version picker.
func (r *Repo) LogFile(ctx context.Context, relPath string, n int) ([]Commit, error) {
	if !r.Available() {
		return nil, ErrNoRepo
	}
	if n <= 0 {
		n = 20
	}
	const sep = "\x1f"
	format := strings.Join([]string{"%h", "%s", "%an", "%aI"}, sep)
	out, err := r.run(ctx, "log", "-n", strconv.Itoa(n), "--format="+format, "--", relPath)
	if err != nil {
		return nil, nil
	}
	var commits []Commit
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, sep, 4)
		if len(parts) != 4 {
			continue
		}
		t, _ := time.Parse(time.RFC3339, parts[3])
		commits = append(commits, Commit{
			SHA:     parts[0],
			Subject: parts[1],
			Author:  parts[2],
			Date:    t,
		})
	}
	return commits, nil
}

// Commit is a single commit record.
type Commit struct {
	SHA     string    `json:"sha"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
}

// EnsureGitattributes ensures .gitattributes contains the merge=union rule
// for the event log. This is a prerequisite for "remote = git" to hold
// (design doc §6.2).
func EnsureGitattributes(root string) error {
	return ensureLine(filepath.Join(root, ".gitattributes"), GitattributesLine)
}

// GitattributesLine is the rule line that must be present.
const GitattributesLine = ".artx/comments/*.yaml merge=union"

// EnsureGitignore ensures .gitignore contains the rule that keeps
// .artx/serve.lock out of the repo. serve.lock carries the --token secret in
// plaintext (blueprint §6.4); its 0600 permissions only keep other local
// users out, they do nothing to stop it from being committed by a stray
// `git add -A`.
func EnsureGitignore(root string) error {
	return ensureLine(filepath.Join(root, ".gitignore"), GitignoreLine)
}

// GitignoreLine is the rule line that must be present.
const GitignoreLine = ".artx/serve.lock"

// ensureLine appends line to the file at path if it isn't already present on
// a line of its own, preserving any existing content and creating the file
// if needed. It is idempotent: calling it again with the same line is a
// no-op.
func ensureLine(path, line string) error {
	existing, err := readFileIfExists(path)
	if err != nil {
		return err
	}
	for _, l := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(l) == line {
			return nil
		}
	}
	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += line + "\n"
	return writeFile(path, []byte(content))
}

func readFileIfExists(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
