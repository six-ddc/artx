package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/six-ddc/art/internal/anchor"
	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/eventlog"
	"github.com/six-ddc/art/internal/mdsrc"
	"github.com/six-ddc/art/internal/render"
	"github.com/six-ddc/art/internal/vault"
)

// ---------------------------------------------------------------------------
// fakeVault: an in-memory implementation of vaultFacade for handler-level unit
// tests, without depending on the real internal/vault implementation (which
// still panics at the skeleton stage).
// ---------------------------------------------------------------------------

type fakeVault struct {
	artifacts map[string]*vault.Artifact
	sources   map[string][]byte
	author    string

	// threadsResp overrides the return value of Threads(); when nil it falls
	// back to the default empty CommentsResponse. Used to construct the
	// "upstream gave us a nil slice" scenario in tests (which can happen at
	// the fold stage) -- the server must guard against it rather than
	// serializing it straight to a JSON null.
	threadsResp *api.CommentsResponse
}

func newFakeVault() *fakeVault {
	return &fakeVault{artifacts: map[string]*vault.Artifact{}, sources: map[string][]byte{}, author: "cappu"}
}

func (f *fakeVault) put(a *vault.Artifact, src []byte) {
	f.artifacts[a.ID] = a
	f.artifacts[a.Slug] = a
	f.sources[a.ID] = src
}

func (f *fakeVault) Scan() ([]vault.Artifact, error) { return nil, nil }

func (f *fakeVault) Lookup(ref string) (*vault.Artifact, error) {
	if a, ok := f.artifacts[ref]; ok {
		return a, nil
	}
	return nil, vault.ErrNotFound
}

func (f *fakeVault) New(slug, typ, title string) (*vault.Artifact, error) {
	a := &vault.Artifact{ID: "newdoc", Slug: slug, Type: typ, Title: title, Path: "/tmp/" + slug}
	f.put(a, nil)
	return a, nil
}

func (f *fakeVault) ReadSource(a *vault.Artifact) ([]byte, error) {
	if src, ok := f.sources[a.ID]; ok {
		return src, nil
	}
	return nil, vault.ErrNotFound
}

func (f *fakeVault) ResolvePath(rel string) (string, error) { return rel, nil }

func (f *fakeVault) Docs(ctx context.Context, baseURL string) ([]api.Doc, error) { return nil, nil }

func (f *fakeVault) Doc(ctx context.Context, a *vault.Artifact, baseURL string) (api.Doc, error) {
	return api.Doc{ID: a.ID, Slug: a.Slug, Type: a.Type, Title: a.Title}, nil
}

func (f *fakeVault) Threads(ctx context.Context, a *vault.Artifact) (*api.CommentsResponse, error) {
	if f.threadsResp != nil {
		return f.threadsResp, nil
	}
	return &api.CommentsResponse{Doc: a.ID}, nil
}

func (f *fakeVault) AllThreads(ctx context.Context, status string) ([]api.Thread, error) {
	return nil, nil
}

func (f *fakeVault) FindThread(ctx context.Context, threadRef string) (*vault.Artifact, *api.Thread, error) {
	return nil, nil, vault.ErrNotFound
}

func (f *fakeVault) Author() string { return f.author }

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestServerWithVault(t *testing.T, fv *fakeVault) (*Server, *fakeAppender) {
	t.Helper()
	fake := &fakeAppender{}
	w := newTestWriter(fake)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go w.Run(ctx)

	s := &Server{
		opts:     Options{Vault: &vault.Vault{Root: t.TempDir(), Name: "test"}},
		vault:    fv,
		hub:      NewHub(),
		renderer: render.New(),
		writer:   w,
	}
	return s, fake
}

func withFixedIDs(t *testing.T) {
	t.Helper()
	origThread, origReply, origEvent := genThreadID, genReplyID, genEventID
	genThreadID = func() string { return "cabcde" }
	genReplyID = func(thread string) string { return thread + ".xyz" }
	genEventID = func() string { return "evt0001" }
	t.Cleanup(func() {
		genThreadID, genReplyID, genEventID = origThread, origReply, origEvent
	})
}

const mdSample = "# 标题\n\n第一段正文，供选区测试使用。\n"

func mdSelectionForParagraph(t *testing.T) api.SelectionInput {
	t.Helper()
	doc, err := mdsrc.Parse([]byte(mdSample))
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range doc.Blocks {
		if b.Kind == "Paragraph" || b.Kind == "TextBlock" {
			return api.SelectionInput{BlockStart: b.Start, BlockEnd: b.End, Exact: "第一段正文，供选区测试使用。"}
		}
	}
	t.Fatal("no paragraph block found")
	return api.SelectionInput{}
}

func decodeEventResponse(t *testing.T, w *httptest.ResponseRecorder) api.EventResponse {
	t.Helper()
	var resp api.EventResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	return resp
}

func postEvent(s *Server, id string, req api.EventRequest) *httptest.ResponseRecorder {
	body, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/docs/"+id+"/events", bytes.NewReader(body))
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.handleDocEvents(w, r)
	return w
}

// ---------------------------------------------------------------------------
// POST /api/docs/{id}/events: one test case for each of the six types.
// ---------------------------------------------------------------------------

func TestHandleDocEventsCreateMD(t *testing.T) {
	withFixedIDs(t)
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "doc001", Slug: "doc", Type: api.DocTypeMD}, []byte(mdSample))
	s, fake := newTestServerWithVault(t, fv)

	sel := mdSelectionForParagraph(t)
	w := postEvent(s, "doc001", api.EventRequest{Type: eventlog.KindCreate, Body: "评论内容", Selection: &sel})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	resp := decodeEventResponse(t, w)
	if resp.Thread != "cabcde" {
		t.Fatalf("thread = %q, want cabcde", resp.Thread)
	}
	if resp.Status != api.StatusOpen {
		t.Fatalf("status = %q, want open", resp.Status)
	}
	if fake.count() != 1 {
		t.Fatalf("expected 1 event written, got %d", fake.count())
	}
	ev := fake.events[0]
	if ev.E != eventlog.KindCreate || ev.Thread != "cabcde" || ev.Anchor == nil {
		t.Fatalf("event written does not match expectations: %+v", ev)
	}
	if ev.Anchor.Kind != api.AnchorText {
		t.Fatalf("md doc anchor Kind should be text, got %q", ev.Anchor.Kind)
	}
	if ev.Author != "cappu" {
		t.Fatalf("author in local mode should come from vault.Author(), got %q", ev.Author)
	}
}

func TestHandleDocEventsCreateMissingSelectionIs400(t *testing.T) {
	withFixedIDs(t)
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "doc001", Slug: "doc", Type: api.DocTypeMD}, []byte(mdSample))
	s, fake := newTestServerWithVault(t, fv)

	w := postEvent(s, "doc001", api.EventRequest{Type: eventlog.KindCreate, Body: "评论内容"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create on md doc missing selection should be 400, got %d: %s", w.Code, w.Body.String())
	}
	var errResp api.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatal(err)
	}
	if errResp.Error != api.ErrBadRequest {
		t.Fatalf("error code = %q, want bad_request", errResp.Error)
	}
	if fake.count() != 0 {
		t.Fatal("no event should be written on a 400")
	}
}

func TestHandleDocEventsReply(t *testing.T) {
	withFixedIDs(t)
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "doc001", Slug: "doc", Type: api.DocTypeMD}, []byte(mdSample))
	s, fake := newTestServerWithVault(t, fv)

	w := postEvent(s, "doc001", api.EventRequest{Type: eventlog.KindReply, Thread: "cabcde", Body: "回复内容"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	resp := decodeEventResponse(t, w)
	if resp.Thread != "cabcde" {
		t.Fatalf("thread = %q", resp.Thread)
	}
	if fake.count() != 1 || fake.events[0].E != eventlog.KindReply || fake.events[0].ID != "cabcde.xyz" {
		t.Fatalf("reply event does not match expectations: %+v", fake.events)
	}
}

func TestHandleDocEventsEdit(t *testing.T) {
	withFixedIDs(t)
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "doc001", Slug: "doc", Type: api.DocTypeMD}, []byte(mdSample))
	s, fake := newTestServerWithVault(t, fv)

	w := postEvent(s, "doc001", api.EventRequest{Type: eventlog.KindEdit, Target: "cabcde.xyz", Body: "修改后的内容"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	resp := decodeEventResponse(t, w)
	if resp.Thread != "cabcde" {
		t.Fatalf("edit should derive the owning thread from target, got %q", resp.Thread)
	}
	if fake.count() != 1 || fake.events[0].E != eventlog.KindEdit || fake.events[0].Target != "cabcde.xyz" {
		t.Fatalf("edit event does not match expectations: %+v", fake.events)
	}
}

func TestHandleDocEventsAddressed(t *testing.T) {
	withFixedIDs(t)
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "doc001", Slug: "doc", Type: api.DocTypeMD}, []byte(mdSample))
	s, fake := newTestServerWithVault(t, fv)

	w := postEvent(s, "doc001", api.EventRequest{Type: eventlog.KindAddressed, Thread: "cabcde", Commit: "deadbee"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	resp := decodeEventResponse(t, w)
	if resp.Status != api.StatusAddressed {
		t.Fatalf("status = %q, want addressed", resp.Status)
	}
	if fake.count() != 1 || fake.events[0].E != eventlog.KindAddressed || fake.events[0].Commit != "deadbee" {
		t.Fatalf("addressed event does not match expectations: %+v", fake.events)
	}
}

func TestHandleDocEventsResolve(t *testing.T) {
	withFixedIDs(t)
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "doc001", Slug: "doc", Type: api.DocTypeMD}, []byte(mdSample))
	s, fake := newTestServerWithVault(t, fv)

	w := postEvent(s, "doc001", api.EventRequest{Type: eventlog.KindResolve, Thread: "cabcde"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	resp := decodeEventResponse(t, w)
	if resp.Status != api.StatusResolved {
		t.Fatalf("status = %q, want resolved", resp.Status)
	}
	if fake.count() != 1 || fake.events[0].E != eventlog.KindResolve {
		t.Fatalf("resolve event does not match expectations: %+v", fake.events)
	}
}

func TestHandleDocEventsReopen(t *testing.T) {
	withFixedIDs(t)
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "doc001", Slug: "doc", Type: api.DocTypeMD}, []byte(mdSample))
	s, fake := newTestServerWithVault(t, fv)

	w := postEvent(s, "doc001", api.EventRequest{Type: eventlog.KindReopen, Thread: "cabcde", Note: "重新打开"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	resp := decodeEventResponse(t, w)
	if resp.Status != api.StatusOpen {
		t.Fatalf("status = %q, want open", resp.Status)
	}
	if fake.count() != 1 || fake.events[0].E != eventlog.KindReopen || fake.events[0].Note != "重新打开" {
		t.Fatalf("reopen event does not match expectations: %+v", fake.events)
	}
}

func TestHandleDocEventsHistoricalVersionConflict(t *testing.T) {
	withFixedIDs(t)
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "doc001", Slug: "doc", Type: api.DocTypeMD}, []byte(mdSample))
	s, fake := newTestServerWithVault(t, fv)

	body, _ := json.Marshal(api.EventRequest{Type: eventlog.KindResolve, Thread: "cabcde"})
	r := httptest.NewRequest(http.MethodPost, "/api/docs/doc001/events?v=deadbeef", bytes.NewReader(body))
	r.SetPathValue("id", "doc001")
	w := httptest.NewRecorder()
	s.handleDocEvents(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("POST events on a historical version should be 409, got %d", w.Code)
	}
	if fake.count() != 0 {
		t.Fatal("no event should be written on a 409")
	}
}

// ---------------------------------------------------------------------------
// Anchor fallback path: safeFromSelection/safeFromElement are the server's
// defensive wrappers around the anchor package -- whether anchor is not yet
// implemented (panics), returns an error, or gives a normal exact match, the
// caller must never crash, and on failure must fall back to an Approx=true
// block-level/whole-doc anchor (the fallback path explicitly allowed by
// blueprint.md §10 risk 3).
// ---------------------------------------------------------------------------

func TestSafeFromSelectionFallsBackWithoutPanicking(t *testing.T) {
	doc, err := mdsrc.Parse([]byte(mdSample))
	if err != nil {
		t.Fatal(err)
	}
	var block *mdsrc.Block
	for i := range doc.Blocks {
		if doc.Blocks[i].Kind == "Paragraph" || doc.Blocks[i].Kind == "TextBlock" {
			block = &doc.Blocks[i]
			break
		}
	}
	if block == nil {
		t.Fatal("no paragraph block found")
	}

	s := &Server{}
	var a anchor.Anchor
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("safeFromSelection must not propagate a panic to the caller: %v", r)
			}
		}()
		a = s.safeFromSelection(doc, api.SelectionInput{BlockStart: block.Start, BlockEnd: block.End})
	}()
	if !a.Approx {
		t.Fatal("at the anchor package's skeleton stage, this should fall back to a block-level Approx=true anchor")
	}
	if a.Start != block.Start || a.End != block.End {
		t.Fatalf("fallback anchor should equal the block range: got [%d,%d), want [%d,%d)", a.Start, a.End, block.Start, block.End)
	}
}

// anchor.FromElement has been implemented by W-anchor: a normal input should
// pass its result straight through (not fall back).
func TestSafeFromElementPassesThroughRealResult(t *testing.T) {
	s := &Server{}
	a := s.safeFromElement(api.ElementInput{AID: "b2c9x1", Quote: "some text"})
	if a.Kind != api.AnchorElement || a.AID != "b2c9x1" || a.Exact != "some text" {
		t.Fatalf("should pass through the real result of anchor.FromElement: %+v", a)
	}
}

// anchor.FromElement returns ErrNoMatch for an empty AID; safeFromElement must
// fall back to an Approx=true anchor instead of propagating the error to the
// caller.
func TestSafeFromElementFallsBackOnError(t *testing.T) {
	s := &Server{}
	var a anchor.Anchor
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("safeFromElement must not propagate a panic to the caller: %v", r)
			}
		}()
		a = s.safeFromElement(api.ElementInput{AID: "", Quote: "some text"})
	}()
	if !a.Approx || a.Kind != api.AnchorElement || a.Exact != "some text" {
		t.Fatalf("fallback element anchor does not match expectations: %+v", a)
	}
}

// ---------------------------------------------------------------------------
// GET /raw/{id}/{path...}: path traversal validation.
// ---------------------------------------------------------------------------

func TestResolveWithinRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if _, ok := resolveWithin(root, "../../etc/passwd"); !ok {
		t.Fatal("resolveWithin should always return a safe path (no error, but must be confined to root)")
	}
	full, _ := resolveWithin(root, "../../etc/passwd")
	if !strings.HasPrefix(full, root) {
		t.Fatalf("resolved path escaped root: %s (root=%s)", full, root)
	}

	// A legitimate subpath should resolve normally to the corresponding
	// location under root.
	full2, ok := resolveWithin(root, "assets/logo.png")
	if !ok || full2 != filepath.Join(root, "assets", "logo.png") {
		t.Fatalf("legitimate path resolved incorrectly: %s ok=%v", full2, ok)
	}
}

func TestHandleRawPathTraversalReturns404(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>hi</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "logo.png"), []byte("PNGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "html01", Slug: "demo", Type: api.DocTypeHTML, Dir: root, Path: filepath.Join(root, "index.html")}, []byte("<html>hi</html>"))
	s, _ := newTestServerWithVault(t, fv)

	// Traversal attempt: inject via SetPathValue directly, bypassing net/http
	// ServeMux's automatic cleaning/redirect of "..", so the test actually
	// exercises the resolveWithin defense inside the handler.
	r := httptest.NewRequest(http.MethodGet, "/raw/html01/../../etc/passwd", nil)
	r.SetPathValue("id", "html01")
	r.SetPathValue("path", "../../etc/passwd")
	w := httptest.NewRecorder()
	s.handleRaw(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("path traversal should be 404, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "root:") {
		t.Fatal("response must not contain the contents of /etc/passwd")
	}

	// A legitimate static asset should still be fetchable.
	r2 := httptest.NewRequest(http.MethodGet, "/raw/html01/assets/logo.png", nil)
	r2.SetPathValue("id", "html01")
	r2.SetPathValue("path", "assets/logo.png")
	w2 := httptest.NewRecorder()
	s.handleRaw(w2, r2)
	if w2.Code != http.StatusOK || w2.Body.String() != "PNGDATA" {
		t.Fatalf("legitimate static asset should be fetchable, got %d body=%q", w2.Code, w2.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Non-/api paths fall back to index.html.
// ---------------------------------------------------------------------------

func TestNonAPIPathFallsBackToIndexHTML(t *testing.T) {
	fv := newFakeVault()
	s, _ := newTestServerWithVault(t, fv)

	for _, path := range []string{"/", "/a/doc001", "/some/random/spa/route"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("%s: Content-Type = %q, want text/html", path, ct)
		}
	}
}
