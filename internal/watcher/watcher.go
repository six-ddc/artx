// Package watcher watches vault files for changes and drives the chain
// "diff → remap → aid injection → auto-commit".
//
// Owner: W-anchor.
//
// It is the fallback for the design principle "bypassing us is harmless":
// any tool editing vault files directly cannot corrupt data — correctness
// is repaired by this package after the fact, rather than enforced on the
// write path.
//
// Self-trigger protection (mandatory, otherwise the watcher loops forever):
//   - both aid injection and auto-commit fire another round of fsnotify events
//   - so this package records the path + content digest of what it just
//     wrote; an event whose on-disk content still matches that digest is
//     dropped outright — if the content no longer matches, someone really
//     did edit the file and the event must go through
//   - changes under .artx/ are always ignored (comment files are owned by
//     serve's single writer)
package watcher

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/eventlog"
	"github.com/six-ddc/artx/internal/gitx"
	"github.com/six-ddc/artx/internal/htmlaid"
	"github.com/six-ddc/artx/internal/remap"
	"github.com/six-ddc/artx/internal/vault"
)

// Notice.Kind values, corresponding to api.SSEDocChange.Kind.
const (
	kindContent = "content"
	kindRemap   = "remap"
	kindAID     = "aid"
)

// defaultDebounce is the debounce window mandated by the design doc.
const defaultDebounce = 300 * time.Millisecond

// Notice is broadcast after a processing pass completes; serve uses it to
// push SSE events.
// Named deliberately to avoid confusion with eventlog.Event: it is not an
// event-log entry.
type Notice struct {
	Kind    string // one of api.SSEDoc's Kind values: content | remap | aid | remove
	DocID   string
	Path    string
	Rev     string
	Remaps  int
	Orphans int
	Threads []string // ids of affected threads
}

// Options configures a Watcher.
type Options struct {
	Vault      *vault.Vault
	Debounce   time.Duration // 0 means 300ms
	AutoCommit bool
	InjectAID  bool
	Remap      remap.Options

	// Emit is called after each processing pass, used to broadcast SSE.
	// May be nil. Implementations must guarantee it never blocks the
	// watcher's main loop (serve uses a buffered channel for this).
	Emit func(Notice)
}

// deps collects all external side effects into function values; New binds
// them to real implementations, tests swap in stubs.
// Kept on an unexported field rather than in Options, so as not to touch
// Options' exported contract.
type deps struct {
	readFile  func(path string) ([]byte, error)
	writeFile func(path string, data []byte, perm os.FileMode) error

	// prevSource fetches the previous version's content from git. Returning
	// (nil, nil) means there is no previous version, so remapping is skipped.
	prevSource func(ctx context.Context, a *vault.Artifact) ([]byte, error)
	// threads fetches the document's folded threads.
	threads func(ctx context.Context, a *vault.Artifact) ([]api.Thread, error)
	// appendEvents persists remap/orphan events.
	appendEvents func(docID string, evs []eventlog.Event) error
	// commit performs the auto-commit, returning the new sha.
	commit func(ctx context.Context, a *vault.Artifact, sum commitSummary) (string, error)
	// artifactAt resolves a changed path to an artifact; returns nil for
	// paths that are not an artifact.
	artifactAt func(path string) (*vault.Artifact, error)
	// scan lists every artifact.
	scan func() ([]vault.Artifact, error)
}

// Watcher watches for and processes vault changes.
type Watcher struct {
	opts Options
	fsw  *fsnotify.Watcher

	deps deps

	mu sync.Mutex
	// selfWrites records the paths this package just wrote and a digest of
	// what was written, used to drop the fsnotify events they trigger
	// without swallowing a real edit that follows right behind them.
	selfWrites map[string]selfWrite
}

// selfWrite is a record of one write-back by this package: the time it
// happened plus a digest of what was written.
type selfWrite struct {
	at  time.Time
	sum [32]byte
}

// New creates a watcher and registers a recursive watch under the vault root.
func New(opts Options) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{opts: opts, fsw: fsw, selfWrites: map[string]selfWrite{}}
	w.deps = realDeps(opts.Vault)
	if opts.Vault != nil {
		if err := w.addTree(opts.Vault.Root); err != nil {
			_ = fsw.Close()
			return nil, err
		}
	}
	return w, nil
}

// realDeps binds deps to the real vault / gitx / eventlog implementations.
func realDeps(v *vault.Vault) deps {
	d := deps{
		readFile:  os.ReadFile,
		writeFile: os.WriteFile,
	}
	d.prevSource = func(ctx context.Context, a *vault.Artifact) ([]byte, error) {
		if v == nil || v.Git == nil || !v.Git.Available() {
			return nil, nil
		}
		b, err := v.Git.ShowFile(ctx, "HEAD", a.RelPath)
		if err != nil {
			// The file not yet being tracked by git (a brand-new document)
			// is not an error, it just means there is no previous version
			// to compare against.
			return nil, nil
		}
		return b, nil
	}
	d.threads = func(ctx context.Context, a *vault.Artifact) ([]api.Thread, error) {
		if v == nil {
			return nil, nil
		}
		resp, err := v.Threads(ctx, a)
		if err != nil || resp == nil {
			return nil, err
		}
		return resp.Threads, nil
	}
	d.appendEvents = func(docID string, evs []eventlog.Event) error {
		if v == nil || v.Store == nil {
			return nil
		}
		return v.Store.Append(docID, evs...)
	}
	d.commit = func(ctx context.Context, a *vault.Artifact, sum commitSummary) (string, error) {
		if v == nil || v.Git == nil || !v.Git.Available() {
			return "", nil
		}
		return v.Git.Commit(ctx, gitCommitOptions(a, sum))
	}
	d.artifactAt = func(path string) (*vault.Artifact, error) {
		if v == nil {
			return nil, nil
		}
		slug := slugOf(v.Root, path)
		if slug == "" {
			return nil, nil
		}
		return v.Lookup(slug)
	}
	d.scan = func() ([]vault.Artifact, error) {
		if v == nil {
			return nil, nil
		}
		return v.Scan()
	}
	return d
}

// Run blocks, processing events until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	debounce := w.debounce()
	timer := time.NewTimer(debounce)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	pending := map[string]bool{}
	armed := false

	for {
		select {
		case <-ctx.Done():
			return nil

		case ev, ok := <-w.fsw.Events:
			if !ok {
				return nil
			}
			if ev.Op&fsnotify.Create != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					_ = w.addTree(ev.Name)
				}
			}
			if !w.shouldProcess(ev.Name) {
				continue
			}
			pending[ev.Name] = true
			if armed && !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounce)
			armed = true

		case _, ok := <-w.fsw.Errors:
			if !ok {
				return nil
			}
			// fsnotify errors are usually a single dropped event; the next
			// change will catch up, so the loop keeps running.

		case <-timer.C:
			armed = false
			paths := make([]string, 0, len(pending))
			for p := range pending {
				paths = append(paths, p)
			}
			pending = map[string]bool{}
			w.processPaths(ctx, paths)
		}
	}
}

// Close releases the fsnotify resources.
func (w *Watcher) Close() error {
	if w.fsw == nil {
		return nil
	}
	return w.fsw.Close()
}

// Process runs one full processing pass for a single artifact. Called
// internally by Run, and can also be called by serve at startup against
// every document to repair any drift that happened while offline.
//
// Steps:
//  1. Read the current content from disk.
//  2. If html and InjectAID: htmlaid.Inject, writing back only if ids were added.
//  3. Fetch the previous version's content from git (gitx.ShowFile HEAD);
//     skip remapping if there is no git.
//  4. remap.Remap every non-resolved thread.
//  5. Append remap/orphan events via eventlog.Store.Append.
//  6. AutoCommit: gitx.Commit(AuthorArtx/AuthorAgent).
//  7. Assemble and Emit(Notice).
func (w *Watcher) Process(ctx context.Context, a *vault.Artifact) (Notice, error) {
	n := Notice{Kind: kindContent, DocID: a.ID, Path: a.Path}
	var sum commitSummary

	src, err := w.deps.readFile(a.Path)
	if err != nil {
		return n, err
	}

	// 2. aid injection. Mark the self-write before writing back, otherwise
	// this write-back would look like a fresh change to ourselves.
	//
	// The element-level data-aid and the document-level <meta name="aid">
	// are backfilled together: when an agent rewrites an html artifact
	// wholesale it also wipes out the meta tag, and that's exactly where
	// the document id lives — without restoring it, this artifact would
	// get a new identity on the next scan and every comment on it would go
	// stale.
	if a.Type == api.DocTypeHTML && w.opts.InjectAID {
		res, err := htmlaid.Inject(src)
		if err != nil {
			return n, err
		}
		out, changed := res.Output, res.Changed
		sum.elementIDsInjected = res.Changed

		// vault.Scan reads the document id only from the file, so once the
		// meta tag is gone, this artifact looks like a brand-new document
		// on the next scan and every comment attached to it goes stale.
		// The last committed version still has that id, so recover it from there.
		wantID := a.ID
		if wantID == "" {
			if prev, perr := w.deps.prevSource(ctx, a); perr == nil && len(prev) > 0 {
				if pid, perr := htmlaid.ExtractDocAID(prev); perr == nil {
					wantID = pid
				}
			}
		}
		if wantID != "" {
			cur, err := htmlaid.ExtractDocAID(out)
			if err != nil {
				return n, err
			}
			if cur != wantID {
				out, err = htmlaid.SetDocAID(out, wantID)
				if err != nil {
					return n, err
				}
				changed = true
				sum.docIDRestored = true
			}
			a.ID, n.DocID = wantID, wantID
		}

		if changed {
			if err := w.deps.writeFile(a.Path, out, 0o644); err != nil {
				return n, err
			}
			w.noteSelfWrite(a.Path, out)
			src = out
			n.Kind = kindAID
		}
	}

	// 3-5. Remapping.
	old, err := w.deps.prevSource(ctx, a)
	if err != nil {
		return n, err
	}
	// No previous version in git means this file was dropped into the vault
	// by hand — `artx new` skeletons are committed at creation time — so the
	// auto-commit is the one that first tracks it.
	sum.firstVersion = len(old) == 0
	if len(old) > 0 && !bytes.Equal(old, src) {
		threads, err := w.deps.threads(ctx, a)
		if err != nil {
			return n, err
		}
		results, err := remap.Remap(old, src, threads, w.opts.Remap)
		if err != nil {
			return n, err
		}
		for _, r := range results {
			switch r.Kind {
			case remap.KindMoved, remap.KindRevived:
				n.Remaps++
				n.Threads = append(n.Threads, r.Thread)
			case remap.KindOrphan:
				n.Orphans++
				n.Threads = append(n.Threads, r.Thread)
			}
		}
		if evs := remap.Events("", results); len(evs) > 0 {
			if err := w.deps.appendEvents(a.ID, evs); err != nil {
				return n, err
			}
			n.Kind = kindRemap
		}
	}

	// 6. auto-commit.
	//
	// This step must NOT set the self-write mark: git add/commit only
	// touches the index and .git/ (already blocked by Ignore), never the
	// working tree files. Marking it would discard every real human edit
	// made during the following window, and there is no mechanism to
	// recover a discarded event — it would show up as "the watcher plays
	// dead right after you edit the file." The self-write mark belongs
	// only to the actual write-back in step 2.
	if w.opts.AutoCommit {
		sum.remapped, sum.orphaned = n.Remaps, n.Orphans
		sha, err := w.deps.commit(ctx, a, sum)
		if err != nil {
			return n, err
		}
		n.Rev = sha
	}

	// 7. Broadcast.
	if w.opts.Emit != nil {
		w.opts.Emit(n)
	}
	return n, nil
}

// ProcessAll runs Process once for every artifact.
func (w *Watcher) ProcessAll(ctx context.Context) ([]Notice, error) {
	arts, err := w.deps.scan()
	if err != nil {
		return nil, err
	}
	out := make([]Notice, 0, len(arts))
	for i := range arts {
		n, err := w.Process(ctx, &arts[i])
		if err != nil {
			// One document failing to process shouldn't sink the whole
			// scan — this is a startup-time repair pass.
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// Ignore reports whether an absolute path should be ignored (.artx/, .git/,
// editor temp files, non-artifact entries).
func Ignore(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return true
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return false
	}
	if strings.HasPrefix(rel, "../") || rel == ".." {
		return true // outside the vault
	}
	parts := strings.Split(rel, "/")
	for _, p := range parts[:len(parts)-1] {
		if strings.HasPrefix(p, ".") {
			return true // .artx/, .git/, and any other hidden directory
		}
	}

	base := parts[len(parts)-1]
	if isTempName(base) {
		return true
	}
	// Directories themselves must be allowed through, otherwise recursive
	// watch registration would get blocked.
	if !strings.Contains(base, ".") {
		return false
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".md", ".html", ".htm":
		return false
	}
	return true
}

// isTempName covers the common intermediate-file naming schemes of popular editors.
func isTempName(base string) bool {
	if base == "" {
		return true
	}
	if strings.HasPrefix(base, ".") {
		return true // .index.md.swp, .#emacs-lock, etc.
	}
	if strings.HasSuffix(base, "~") {
		return true // emacs / gedit backups
	}
	if base == "4913" {
		return true // vim's writability probe directory
	}
	if strings.HasPrefix(base, "#") && strings.HasSuffix(base, "#") {
		return true // emacs autosave
	}
	if strings.Contains(base, "___jb_tmp___") || strings.Contains(base, "___jb_old___") {
		return true // JetBrains atomic write
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".swp", ".swo", ".swx", ".tmp", ".temp", ".bak", ".orig", ".part", ".crdownload":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func (w *Watcher) debounce() time.Duration {
	if w.opts.Debounce > 0 {
		return w.opts.Debounce
	}
	return defaultDebounce
}

// selfWriteWindow is how long a self-write mark stays valid. Twice the
// debounce window: a single write-back can produce multiple events
// (WRITE + CHMOD, etc.) and fsnotify delivery can lag, so too narrow a
// window would miss the tail of them.
func (w *Watcher) selfWriteWindow() time.Duration {
	d := 2 * w.debounce()
	if d < time.Second {
		d = time.Second
	}
	return d
}

// noteSelfWrite is called right after writing a file back, recording
// exactly what content was written.
func (w *Watcher) noteSelfWrite(path string, content []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.selfWrites[path] = selfWrite{at: time.Now(), sum: sha256.Sum256(content)}
}

// isSelfWrite reports whether a given fsnotify event is just the echo of a
// write-back this package performed.
//
// The test is content, not time: it's only self-triggered if the bytes on
// disk are still the ones we wrote. As soon as someone genuinely edits the
// file within the window, the digest stops matching and the event must go
// through — a purely time-based window would permanently drop real edits
// made during that period, and a dropped event has no way to recover,
// which shows up as "the watcher plays dead right after you edit the
// file." The time window is only used to expire old records, never as a
// blocking condition on its own.
func (w *Watcher) isSelfWrite(path string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	window := w.selfWriteWindow()
	now := time.Now()
	for p, s := range w.selfWrites {
		if now.Sub(s.at) > window {
			delete(w.selfWrites, p)
		}
	}
	s, ok := w.selfWrites[path]
	if !ok || now.Sub(s.at) > window {
		return false
	}
	cur, err := w.deps.readFile(path)
	if err != nil {
		return false
	}
	return sha256.Sum256(cur) == s.sum
}

// shouldProcess is Run's admission check for each fsnotify event.
func (w *Watcher) shouldProcess(path string) bool {
	root := ""
	if w.opts.Vault != nil {
		root = w.opts.Vault.Root
	}
	if Ignore(root, path) {
		return false
	}
	return !w.isSelfWrite(path)
}

func (w *Watcher) processPaths(ctx context.Context, paths []string) {
	seen := map[string]bool{}
	for _, p := range paths {
		a, err := w.deps.artifactAt(p)
		if err != nil || a == nil || seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		_, _ = w.Process(ctx, a)
	}
}

// addTree recursively registers every non-ignored directory under root.
func (w *Watcher) addTree(root string) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if p != root && Ignore(root, p) {
			return filepath.SkipDir
		}
		return w.fsw.Add(p)
	})
}

// slugOf takes the first path segment relative to the vault root, i.e. the artifact's slug.
func slugOf(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || strings.HasPrefix(rel, "../") {
		return ""
	}
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return ""
}

// commitSummary records what a processing pass actually did, so the
// auto-commit message can say more than a bare "update".
type commitSummary struct {
	firstVersion       bool // the file had no previous version in git
	docIDRestored      bool // the <meta name="aid"> doc id was re-established
	elementIDsInjected bool // htmlaid.Inject added element ids
	remapped           int  // threads moved/revived this pass
	orphaned           int  // threads orphaned this pass
}

// message renders the one-line commit subject: an add/update verb plus a
// parenthesized summary of everything notable this pass did, e.g.
// "artx: update design-doc (restore doc id, 2 comments remapped, 1 orphaned)".
func (s commitSummary) message(slug string) string {
	verb := "update"
	if s.firstVersion {
		verb = "add"
	}

	var parts []string
	if s.docIDRestored {
		parts = append(parts, "restore doc id")
	}
	if s.elementIDsInjected {
		parts = append(parts, "inject element ids")
	}
	if s.remapped > 0 {
		parts = append(parts, fmt.Sprintf("%d %s remapped", s.remapped, plural(s.remapped, "comment")))
	}
	if s.orphaned > 0 {
		if s.remapped > 0 {
			// "comments" is already named by the remap part.
			parts = append(parts, fmt.Sprintf("%d orphaned", s.orphaned))
		} else {
			parts = append(parts, fmt.Sprintf("%d %s orphaned", s.orphaned, plural(s.orphaned, "comment")))
		}
	}

	msg := "artx: " + verb + " " + slug
	if len(parts) > 0 {
		msg += " (" + strings.Join(parts, ", ") + ")"
	}
	return msg
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// gitCommitOptions is the fixed shape of the watcher's auto-commit.
// The author is always AuthorArtx: this commit is art itself wrapping up
// (settling an aid injection / remap), not an agent or a human writing content.
func gitCommitOptions(a *vault.Artifact, sum commitSummary) gitx.CommitOptions {
	// The scope is "this artifact's directory + .artx/comments", not `git add -A`.
	//
	// Including .artx/comments is required: the remap/orphan events appended
	// this round live there, and they must land in the same commit as the
	// content change, otherwise the rev and the anchor offsets would drift
	// apart.
	//
	// But it must not be widened further to the whole repo: while
	// processing document A, `add -A` would also commit document B's
	// in-progress edits that the watcher hasn't processed yet, so B's
	// "previous version" gets overwritten by a commit that has nothing to
	// do with it — that's exactly how an html document's id recovery
	// baseline gets lost.
	return gitx.CommitOptions{
		Message: sum.message(a.Slug),
		Author:  gitx.AuthorArtx,
		Paths:   []string{a.Slug, eventlog.CommentsDir},
	}
}

// Reference api's constant explicitly so the intent of the import is clear.
var _ = api.SSEDoc
