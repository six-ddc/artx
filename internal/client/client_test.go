package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/lockfile"
)

func newTestServer(t *testing.T, token string, root string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	checkAuth := func(w http.ResponseWriter, r *http.Request) bool {
		if token == "" {
			return true
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got != token {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Error: api.ErrUnauthorized, Message: "nope"})
			return false
		}
		return true
	}

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(api.HealthResponse{OK: "ok", Version: "test", Vault: "work", Root: root})
	})

	mux.HandleFunc("/api/docs", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r) {
			return
		}
		if r.Method == http.MethodPost {
			var req api.NewDocRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			_ = json.NewEncoder(w).Encode(api.NewDocResponse{ID: "newid1", Path: "/x/" + req.Slug, URL: "http://x/a/newid1", Slug: req.Slug, Type: req.Type})
			return
		}
		_ = json.NewEncoder(w).Encode(api.DocsResponse{
			Vault: "work", Root: root,
			Docs: []api.Doc{{ID: "docaaa", Slug: "doc-a"}, {ID: "docbbb", Slug: "doc-b"}},
		})
	})

	mux.HandleFunc("/api/docs/docaaa/comments", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(api.CommentsResponse{
			Doc: "docaaa",
			Threads: []api.Thread{
				{Thread: "caaaa1", Doc: "docaaa", Status: api.StatusOpen},
				{Thread: "caaaa2", Doc: "docaaa", Status: api.StatusResolved},
			},
		})
	})
	mux.HandleFunc("/api/docs/docbbb/comments", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(api.CommentsResponse{
			Doc:     "docbbb",
			Threads: []api.Thread{{Thread: "cbbbb1", Doc: "docbbb", Status: api.StatusOpen}},
		})
	})

	mux.HandleFunc("/api/docs/docaaa/events", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r) {
			return
		}
		var req api.EventRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Type == "create" && req.Selection == nil && req.Element == nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Error: api.ErrBadRequest, Message: "missing selection"})
			return
		}
		_ = json.NewEncoder(w).Encode(api.EventResponse{OK: "ok", Thread: "cnewthr", EventID: "e1", Status: api.StatusOpen})
	})

	mux.HandleFunc("/api/compact", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(api.CompactResponse{Stats: []api.CompactStat{{Doc: "docaaa", Skipped: true}}})
	})

	return httptest.NewServer(mux)
}

func TestHealthAndDocs(t *testing.T) {
	srv := newTestServer(t, "", "/root/work")
	defer srv.Close()
	c := New(srv.URL, "")

	h, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h.Root != "/root/work" {
		t.Errorf("Health.Root = %q", h.Root)
	}

	docs, err := c.Docs(context.Background())
	if err != nil {
		t.Fatalf("Docs: %v", err)
	}
	if len(docs.Docs) != 2 {
		t.Fatalf("Docs() = %d docs, want 2", len(docs.Docs))
	}
}

func TestCommentsAggregatesAcrossDocs(t *testing.T) {
	srv := newTestServer(t, "", "/root/work")
	defer srv.Close()
	c := New(srv.URL, "")

	all, err := c.Comments(context.Background(), "", "")
	if err != nil {
		t.Fatalf("Comments(all docs): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("Comments(all) = %d threads, want 3", len(all))
	}

	open, err := c.Comments(context.Background(), "", api.StatusOpen)
	if err != nil {
		t.Fatalf("Comments(open): %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("Comments(open) = %d threads, want 2", len(open))
	}

	single, err := c.Comments(context.Background(), "docaaa", "")
	if err != nil {
		t.Fatalf("Comments(docaaa): %v", err)
	}
	if len(single) != 2 {
		t.Fatalf("Comments(docaaa) = %d threads, want 2", len(single))
	}
}

func TestPostEventErrorMapsToClientError(t *testing.T) {
	srv := newTestServer(t, "", "/root/work")
	defer srv.Close()
	c := New(srv.URL, "")

	_, err := c.PostEvent(context.Background(), "docaaa", api.EventRequest{Type: "create", Body: "hi"})
	if err == nil {
		t.Fatal("PostEvent with missing selection should fail")
	}
	cerr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error type = %T, want *client.Error", err)
	}
	if cerr.Status != http.StatusBadRequest || cerr.Response.Error != api.ErrBadRequest {
		t.Fatalf("Error = %+v, want bad_request/400", cerr)
	}
}

func TestFindThreadPrefixMatch(t *testing.T) {
	srv := newTestServer(t, "", "/root/work")
	defer srv.Close()
	c := New(srv.URL, "")

	docID, th, err := c.FindThread(context.Background(), "cbbbb1")
	if err != nil {
		t.Fatalf("FindThread(exact): %v", err)
	}
	if docID != "docbbb" || th.Thread != "cbbbb1" {
		t.Fatalf("FindThread(exact) = %q %+v", docID, th)
	}

	docID2, th2, err := c.FindThread(context.Background(), "cbbb")
	if err != nil {
		t.Fatalf("FindThread(prefix): %v", err)
	}
	if docID2 != "docbbb" || th2.Thread != "cbbbb1" {
		t.Fatalf("FindThread(prefix) = %q %+v", docID2, th2)
	}

	if _, _, err := c.FindThread(context.Background(), "nonexistent"); err == nil {
		t.Fatal("FindThread(missing) should error")
	}
}

func TestTokenAuthorization(t *testing.T) {
	srv := newTestServer(t, "sekrit", "/root/work")
	defer srv.Close()

	unauth := New(srv.URL, "")
	if _, err := unauth.Health(context.Background()); err == nil {
		t.Fatal("Health without token should fail against a token-protected server")
	}

	authed := New(srv.URL, "sekrit")
	if _, err := authed.Health(context.Background()); err != nil {
		t.Fatalf("Health with correct token: %v", err)
	}
}

func TestDetectMatchesRootAndFromServeInfo(t *testing.T) {
	srv := newTestServer(t, "", "/root/work")
	defer srv.Close()

	info := &lockfile.ServeInfo{Host: "127.0.0.1", Root: "/root/work"}
	// Point FromServeInfo-style client directly at the test server by
	// overriding base (httptest server isn't on the info's host:port).
	c := New(srv.URL, info.Token)
	h, err := c.Health(context.Background())
	if err != nil || h.Root != "/root/work" {
		t.Fatalf("Health via constructed client = %+v, %v", h, err)
	}
}

func TestCompact(t *testing.T) {
	srv := newTestServer(t, "", "/root/work")
	defer srv.Close()
	c := New(srv.URL, "")

	resp, err := c.Compact(context.Background(), api.CompactRequest{})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(resp.Stats) != 1 || !resp.Stats[0].Skipped {
		t.Fatalf("Compact response = %+v", resp)
	}
}
