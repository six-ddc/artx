package watcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/eventlog"
	"github.com/six-ddc/art/internal/htmlaid"
	"github.com/six-ddc/art/internal/vault"
)

func TestIgnore(t *testing.T) {
	root := "/vault"
	cases := []struct {
		path string
		want bool
		why  string
	}{
		{"/vault/.art/comments/a7f3k2.yaml", true, "comment files are owned by serve's single writer"},
		{"/vault/.art/serve.lock", true, "the whole .art directory is ignored"},
		{"/vault/.art", true, "the .art directory itself"},
		{"/vault/.git/index", true, "git internal file"},
		{"/vault/.git/refs/heads/main", true, "git internal file"},
		{"/vault/demo/index.md~", true, "editor backup"},
		{"/vault/demo/.index.md.swp", true, "vim swap"},
		{"/vault/demo/index.md.swp", true, "vim swap"},
		{"/vault/demo/4913", true, "vim writability probe"},
		{"/vault/demo/#index.md#", true, "emacs autosave"},
		{"/vault/demo/.#index.md", true, "emacs lock file"},
		{"/vault/demo/index.md___jb_tmp___", true, "JetBrains atomic write"},
		{"/vault/demo/index.md.tmp", true, "temp file"},
		{"/vault/demo/style.css", true, "not an artifact entry"},
		{"/vault/demo/index.md", false, "md entry"},
		{"/vault/demo/index.html", false, "html entry"},
		{"/vault/demo/notes.md", false, "an md file inside the vault"},
		{"/vault/demo", false, "a plain directory must be allowed, otherwise recursive watch registration fails"},
		{"/vault", false, "the root itself"},
		{"/elsewhere/index.md", true, "outside the vault"},
	}
	for _, c := range cases {
		if got := Ignore(root, c.path); got != c.want {
			t.Errorf("Ignore(%q) = %v, want %v (%s)", c.path, got, c.want, c.why)
		}
	}
}

// ---------------------------------------------------------------------------
// Process
// ---------------------------------------------------------------------------

const rawHTML = `<!doctype html>
<html><head><meta name="aid" content="a7f3k2"><title>demo</title></head>
<body><main><h1>标题</h1><p>正文段落。</p></main></body></html>
`

// fixture builds a temporary vault containing a single artifact, and
// returns a Watcher whose deps are already stubbed out.
func fixture(t *testing.T, typ, content string) (*Watcher, *vault.Artifact) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := vault.IndexMD
	if typ == api.DocTypeHTML {
		name = vault.IndexHTML
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &vault.Artifact{
		ID: "a7f3k2", Slug: "demo", Type: typ,
		Dir: dir, Path: path, RelPath: filepath.Join("demo", name),
	}
	w := &Watcher{
		opts: Options{
			Vault:     &vault.Vault{Root: root},
			InjectAID: true,
			Debounce:  20 * time.Millisecond,
		},
		selfWrites: map[string]selfWrite{},
		deps: deps{
			readFile:  os.ReadFile,
			writeFile: os.WriteFile,
			prevSource: func(context.Context, *vault.Artifact) ([]byte, error) {
				return nil, nil
			},
			threads: func(context.Context, *vault.Artifact) ([]api.Thread, error) {
				return nil, nil
			},
			appendEvents: func(string, []eventlog.Event) error { return nil },
			commit: func(context.Context, *vault.Artifact) (string, error) {
				return "", nil
			},
			artifactAt: func(string) (*vault.Artifact, error) { return a, nil },
			scan:       func() ([]vault.Artifact, error) { return []vault.Artifact{*a}, nil },
		},
	}
	return w, a
}

// TestProcessGuardsAgainstSelfExcitation is the acceptance test for risk 3:
// a file Process wrote back itself must not trigger a second processing round.
func TestProcessGuardsAgainstSelfExcitation(t *testing.T) {
	w, a := fixture(t, api.DocTypeHTML, rawHTML)

	n, err := w.Process(context.Background(), a)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if n.Kind != kindAID {
		t.Fatalf("an aid was injected, Notice.Kind should be %q, got %q", kindAID, n.Kind)
	}

	written, err := os.ReadFile(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), htmlaid.AIDAttr) {
		t.Fatalf("aid was not written back to the file: %s", written)
	}

	// The key assertion: the fsnotify event produced by the write-back must
	// be blocked by the self-trigger guard.
	if !w.isSelfWrite(a.Path) {
		t.Fatal("a self-write mark should be set before the write-back")
	}
	if w.shouldProcess(a.Path) {
		t.Fatal("the event triggered by our own write-back should not enter a second processing round")
	}

	// Defense in depth: even if a second round did run, injection is
	// idempotent, so it wouldn't write a third time.
	r, err := htmlaid.Inject(written)
	if err != nil {
		t.Fatal(err)
	}
	if r.Changed {
		t.Fatal("a second injection pass should not add anything, otherwise it would self-trigger forever")
	}
}

func TestSelfWriteMarkExpires(t *testing.T) {
	w, a := fixture(t, api.DocTypeHTML, rawHTML)
	content, err := os.ReadFile(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	w.noteSelfWrite(a.Path, content)
	if !w.isSelfWrite(a.Path) {
		t.Fatal("a freshly recorded write-back should take effect")
	}
	// Roll the record back outside the window; a subsequent real edit must
	// then be processable.
	w.mu.Lock()
	s := w.selfWrites[a.Path]
	s.at = time.Now().Add(-2 * w.selfWriteWindow())
	w.selfWrites[a.Path] = s
	w.mu.Unlock()

	if w.isSelfWrite(a.Path) {
		t.Fatal("an expired record should no longer block the event")
	}
	if !w.shouldProcess(a.Path) {
		t.Fatal("a real edit after expiry should be allowed into processing")
	}
}

// TestSelfWriteReleasesRealEditInsideWindow covers the other half of the
// self-trigger guard: a real edit inside the window must be let through.
// A purely time-based window would drop it forever with no way to recover.
func TestSelfWriteReleasesRealEditInsideWindow(t *testing.T) {
	w, a := fixture(t, api.DocTypeHTML, rawHTML)
	content, err := os.ReadFile(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	w.noteSelfWrite(a.Path, content)

	// The record is still within the window, but someone edits the file
	// immediately after.
	if err := os.WriteFile(a.Path, append(content, []byte("<!-- real edit -->")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if w.isSelfWrite(a.Path) {
		t.Fatal("the content changed, so this should no longer be judged a self-trigger")
	}
	if !w.shouldProcess(a.Path) {
		t.Fatal("a real edit inside the window must be allowed into processing")
	}
}

func TestProcessSkipsAIDInjectionForMarkdown(t *testing.T) {
	w, a := fixture(t, api.DocTypeMD, "# 标题\n\n正文。\n")
	before, _ := os.ReadFile(a.Path)

	n, err := w.Process(context.Background(), a)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if n.Kind != kindContent {
		t.Fatalf("markdown gets no aid injection, Kind should be %q, got %q", kindContent, n.Kind)
	}
	after, _ := os.ReadFile(a.Path)
	if string(before) != string(after) {
		t.Fatal("the watcher should not rewrite md files")
	}
	if w.isSelfWrite(a.Path) {
		t.Fatal("a self-write mark should not be set when nothing was written")
	}
}

func TestProcessEmitsNotice(t *testing.T) {
	w, a := fixture(t, api.DocTypeMD, "# 标题\n")
	var got []Notice
	w.opts.Emit = func(n Notice) { got = append(got, n) }

	if _, err := w.Process(context.Background(), a); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 notice broadcast, got %d", len(got))
	}
	if got[0].DocID != a.ID || got[0].Path != a.Path {
		t.Fatalf("unexpected notice content: %+v", got[0])
	}
}

// TestProcessRemapsOnContentChange depends on W-core's eventlog.NewEvent
// (called internally by remap.Events).
func TestProcessRemapsOnContentChange(t *testing.T) {
	const anchored = "支付重构方案需要评估风险。"
	oldSrc := "# 标题\n\n" + anchored + "\n"
	newSrc := "# 标题\n\n新插入的一段。\n\n" + anchored + "\n"

	w, a := fixture(t, api.DocTypeMD, newSrc)
	w.deps.prevSource = func(context.Context, *vault.Artifact) ([]byte, error) {
		return []byte(oldSrc), nil
	}
	w.deps.threads = func(context.Context, *vault.Artifact) ([]api.Thread, error) {
		s := strings.Index(oldSrc, anchored)
		return []api.Thread{{
			Thread: "c7k2f9", Status: api.StatusOpen,
			Anchor: api.ThreadAnchor{
				Kind: api.AnchorText, Exact: anchored, Start: s, End: s + len(anchored),
			},
		}}, nil
	}
	var appended []eventlog.Event
	w.deps.appendEvents = func(docID string, evs []eventlog.Event) error {
		if docID != a.ID {
			t.Errorf("appendEvents docID = %q, want %q", docID, a.ID)
		}
		appended = append(appended, evs...)
		return nil
	}

	n := processOrSkip(t, w, a)

	if n.Kind != kindRemap {
		t.Fatalf("Kind should be %q after an anchor shift, got %q", kindRemap, n.Kind)
	}
	if n.Remaps != 1 || n.Orphans != 0 {
		t.Fatalf("wrong counts: %+v", n)
	}
	if len(n.Threads) != 1 || n.Threads[0] != "c7k2f9" {
		t.Fatalf("wrong affected threads: %v", n.Threads)
	}
	if len(appended) != 1 || appended[0].E != eventlog.KindRemap {
		t.Fatalf("expected 1 remap event to be appended, got %+v", appended)
	}
	if want := strings.Index(newSrc, anchored); appended[0].Start != want {
		t.Fatalf("remap event's new start = %d, want %d", appended[0].Start, want)
	}
}

// processOrSkip skips the test if eventlog.NewEvent isn't implemented yet (panics).
func processOrSkip(t *testing.T, w *Watcher, a *vault.Artifact) (n Notice) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("eventlog.NewEvent not implemented yet (W-core), skipping: %v", r)
		}
	}()
	got, err := w.Process(context.Background(), a)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	return got
}

func TestProcessAllCoversEveryArtifact(t *testing.T) {
	w, a := fixture(t, api.DocTypeHTML, rawHTML)
	ns, err := w.ProcessAll(context.Background())
	if err != nil {
		t.Fatalf("ProcessAll: %v", err)
	}
	if len(ns) != 1 || ns[0].DocID != a.ID {
		t.Fatalf("unexpected ProcessAll result: %+v", ns)
	}
}

func TestProcessAllSkipsBrokenArtifact(t *testing.T) {
	w, a := fixture(t, api.DocTypeHTML, rawHTML)
	missing := *a
	missing.ID = "gone01"
	missing.Path = filepath.Join(a.Dir, "nope.html")
	w.deps.scan = func() ([]vault.Artifact, error) {
		return []vault.Artifact{missing, *a}, nil
	}

	ns, err := w.ProcessAll(context.Background())
	if err != nil {
		t.Fatalf("one document failing shouldn't error out the whole ProcessAll: %v", err)
	}
	if len(ns) != 1 || ns[0].DocID != a.ID {
		t.Fatalf("the unreadable document should have been skipped, got %+v", ns)
	}
}

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------

func TestRunDebouncesAndProcesses(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, vault.IndexMD)
	if err := os.WriteFile(path, []byte("# 初始\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var calls int
	done := make(chan struct{}, 8)

	a := &vault.Artifact{
		ID: "a7f3k2", Slug: "demo", Type: api.DocTypeMD,
		Dir: dir, Path: path, RelPath: filepath.Join("demo", vault.IndexMD),
	}
	w, err := New(Options{
		Vault:    &vault.Vault{Root: root},
		Debounce: 50 * time.Millisecond,
		Emit: func(Notice) {
			mu.Lock()
			calls++
			mu.Unlock()
			done <- struct{}{}
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	// Swap out the external side effects, keeping only the core chain
	// "watch -> debounce -> Process".
	w.deps.prevSource = func(context.Context, *vault.Artifact) ([]byte, error) { return nil, nil }
	w.deps.artifactAt = func(string) (*vault.Artifact, error) { return a, nil }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	// Write three times in a row: after debouncing, only one round should be processed.
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(path, []byte("# 改动\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Process to be triggered")
	}
	// Wait one more debounce window to confirm there's no extra second round.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("three writes in a row should debounce into 1 processing round, got %d", calls)
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	root := t.TempDir()
	w, err := New(Options{Vault: &vault.Vault{Root: root}, Debounce: 20 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- w.Run(ctx) }()
	cancel()

	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("cancelling ctx should exit cleanly, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after ctx was cancelled")
	}
}

func TestRunIgnoresArtDir(t *testing.T) {
	root := t.TempDir()
	artDir := filepath.Join(root, vault.ArtDir, "comments")
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := New(Options{Vault: &vault.Vault{Root: root}, Debounce: 20 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	var touched int
	var mu sync.Mutex
	w.deps.artifactAt = func(string) (*vault.Artifact, error) {
		mu.Lock()
		touched++
		mu.Unlock()
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	if err := os.WriteFile(filepath.Join(artDir, "a7f3k2.yaml"), []byte("---\ne: create\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if touched != 0 {
		t.Fatalf("changes under .art/ should never reach processing, but it fired %d times", touched)
	}
}
