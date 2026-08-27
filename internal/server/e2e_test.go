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

	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/eventlog"
	"github.com/six-ddc/art/internal/mdsrc"
	"github.com/six-ddc/art/internal/vault"
)

// setupRealVault builds a real *vault.Vault (bypassing vault.Init to avoid the
// side effect of writing the user's global registry at
// ~/.config/art/config.yaml, which tests should never do), for use by
// end-to-end integration tests. Now that vault/eventlog/anchor/htmlaid are all
// real implementations, this path no longer needs any fakes.
func setupRealVault(t *testing.T) *vault.Vault {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, vault.ArtDir), 0o755); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(dir, "e2e")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func newRealServer(t *testing.T, v *vault.Vault) *Server {
	t.Helper()
	s, err := New(Options{Vault: v})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.writer.Run(ctx)
	return s
}

func decodeJSON[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(w.Body).Decode(&v); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	return v
}

// End-to-end: real vault + real eventlog + real anchor, exercising the full
// Handler() routing. Covers the server-side half of the end-to-end script in
// design doc §9.4: create doc -> render -> comment on a selection -> comment
// visible in the list -> resolve -> comment list status updates, all without
// any fakes.
func TestEndToEndRealVaultCreateCommentResolve(t *testing.T) {
	v := setupRealVault(t)
	s := newRealServer(t, v)
	h := s.Handler()

	// 1. GET /api/health reflects real vault info.
	{
		r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("health status=%d body=%s", w.Code, w.Body.String())
		}
		health := decodeJSON[api.HealthResponse](t, w)
		if health.OK != "ok" || health.Root != v.Root {
			t.Fatalf("health mismatch: %+v (want root=%s)", health, v.Root)
		}
	}

	// 2. POST /api/docs creates a md doc via the real vault.New.
	var docID string
	{
		body, _ := json.Marshal(api.NewDocRequest{Slug: "payment-refactor", Type: api.DocTypeMD, Title: "支付重构"})
		r := httptest.NewRequest(http.MethodPost, "/api/docs", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("create doc status=%d body=%s", w.Code, w.Body.String())
		}
		resp := decodeJSON[api.NewDocResponse](t, w)
		if resp.ID == "" || resp.Slug != "payment-refactor" || resp.Type != api.DocTypeMD {
			t.Fatalf("unexpected NewDocResponse: %+v", resp)
		}
		docID = resp.ID
	}

	// Add a body so selection comments have a real paragraph to anchor to.
	a, err := v.Lookup(docID)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("---\naid: " + docID + "\ntitle: 支付重构\n---\n\n# 支付重构\n\n支付网关的选型需要综合考虑成本与稳定性。\n")
	if err := os.WriteFile(a.Path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. GET /api/docs list should include the doc just created.
	{
		r := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("list docs status=%d body=%s", w.Code, w.Body.String())
		}
		resp := decodeJSON[api.DocsResponse](t, w)
		found := false
		for _, d := range resp.Docs {
			if d.ID == docID {
				found = true
			}
		}
		if !found {
			t.Fatalf("newly created doc did not appear in the list: %+v", resp.Docs)
		}
	}

	// 4. GET /api/docs/{id} rendered HTML should carry data-sourcepos and let
	// us locate the paragraph block.
	doc, err := mdsrc.Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	var para *mdsrc.Block
	for i := range doc.Blocks {
		if doc.Blocks[i].Kind == "Paragraph" || doc.Blocks[i].Kind == "TextBlock" {
			para = &doc.Blocks[i]
			break
		}
	}
	if para == nil {
		t.Fatal("no paragraph block found")
	}
	{
		r := httptest.NewRequest(http.MethodGet, "/api/docs/"+docID, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("doc detail status=%d body=%s", w.Code, w.Body.String())
		}
		detail := decodeJSON[api.DocDetail](t, w)
		if !strings.Contains(detail.HTML, "data-sourcepos") {
			t.Fatalf("rendered result is missing data-sourcepos: %s", detail.HTML)
		}
		if detail.Title != "支付重构" {
			t.Fatalf("title = %q", detail.Title)
		}
	}

	// 5. POST events create: real anchor.FromSelection does the in-block quote match.
	var threadID string
	{
		sel := api.SelectionInput{
			BlockStart: para.Start, BlockEnd: para.End,
			Exact: "支付网关的选型需要综合考虑成本与稳定性。",
		}
		body, _ := json.Marshal(api.EventRequest{Type: eventlog.KindCreate, Body: "这段建议拆分", Selection: &sel})
		r := httptest.NewRequest(http.MethodPost, "/api/docs/"+docID+"/events", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("create event status=%d body=%s", w.Code, w.Body.String())
		}
		resp := decodeJSON[api.EventResponse](t, w)
		if resp.Thread == "" || resp.Status != api.StatusOpen {
			t.Fatalf("unexpected EventResponse: %+v", resp)
		}
		threadID = resp.Thread
	}

	// 6. GET comments should show this thread, with an exact-match anchor (Approx=false).
	{
		r := httptest.NewRequest(http.MethodGet, "/api/docs/"+docID+"/comments", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("comments status=%d body=%s", w.Code, w.Body.String())
		}
		resp := decodeJSON[api.CommentsResponse](t, w)
		var found *api.Thread
		for i := range resp.Threads {
			if resp.Threads[i].Thread == threadID {
				found = &resp.Threads[i]
			}
		}
		if found == nil {
			t.Fatalf("thread did not appear in the comments list: %+v", resp.Threads)
		}
		if found.Status != api.StatusOpen {
			t.Fatalf("status = %q, want open", found.Status)
		}
		if found.Anchor.Approx {
			t.Fatalf("selection text can be found exactly in the body, anchor should not be Approx: %+v", found.Anchor)
		}
		if found.Anchor.Exact != "支付网关的选型需要综合考虑成本与稳定性。" {
			t.Fatalf("anchor exact mismatch: %q", found.Anchor.Exact)
		}
	}

	// 7. After resolve, the comments list should reflect the new status.
	{
		body, _ := json.Marshal(api.EventRequest{Type: eventlog.KindResolve, Thread: threadID})
		r := httptest.NewRequest(http.MethodPost, "/api/docs/"+docID+"/events", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("resolve status=%d body=%s", w.Code, w.Body.String())
		}
	}
	{
		r := httptest.NewRequest(http.MethodGet, "/api/docs/"+docID+"/comments", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		resp := decodeJSON[api.CommentsResponse](t, w)
		var found *api.Thread
		for i := range resp.Threads {
			if resp.Threads[i].Thread == threadID {
				found = &resp.Threads[i]
			}
		}
		if found == nil || found.Status != api.StatusResolved {
			t.Fatalf("status should be resolved after resolve: %+v", found)
		}
	}
}

// html artifact: /raw/{id}/ should inject the reviewer script (real
// htmlaid.InjectReviewer), and /raw/{id}/{path...} should serve static
// assets from the same directory.
func TestEndToEndRealVaultHTMLArtifactRaw(t *testing.T) {
	v := setupRealVault(t)
	s := newRealServer(t, v)
	h := s.Handler()

	body, _ := json.Marshal(api.NewDocRequest{Slug: "pricing-demo", Type: api.DocTypeHTML, Title: "Pricing Demo"})
	r := httptest.NewRequest(http.MethodPost, "/api/docs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create html doc status=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeJSON[api.NewDocResponse](t, w)

	a, err := v.Lookup(resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(a.Dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.Dir, "assets", "logo.png"), []byte("PNGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	r2 := httptest.NewRequest(http.MethodGet, "/raw/"+resp.ID+"/", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("raw entry status=%d body=%s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "/_art/reviewer.js") {
		t.Fatalf("html entry point should have the reviewer script injected: %s", w2.Body.String())
	}

	r3 := httptest.NewRequest(http.MethodGet, "/raw/"+resp.ID+"/assets/logo.png", nil)
	r3.SetPathValue("id", resp.ID)
	r3.SetPathValue("path", "assets/logo.png")
	w3 := httptest.NewRecorder()
	s.handleRaw(w3, r3)
	if w3.Code != http.StatusOK || w3.Body.String() != "PNGDATA" {
		t.Fatalf("failed to fetch static asset: status=%d body=%q", w3.Code, w3.Body.String())
	}
}
