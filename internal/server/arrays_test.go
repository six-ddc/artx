package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/vault"
)

// Audit finding (related to BLOCKER-1): fields that are arrays in the contract
// and whose json tag has no omitempty (api.DocsResponse.Docs,
// api.CommentsResponse.Threads, api.Thread.Replies, api.CompactResponse.Stats,
// and the hand-written "commits" in /api/docs/{id}/history) get serialized by
// encoding/json as a bare null whenever the underlying value is a nil slice.
// When the frontend consumes them as arrays (.map/.length), the whole page
// crashes. Add a regression assertion for each one: the raw response bytes
// must contain "[]", never "null".

func TestDocsListNeverSerializesDocsAsNull(t *testing.T) {
	fv := newFakeVault() // fakeVault.Docs returns (nil, nil) by default
	s, _ := newTestServerWithVault(t, fv)

	r := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	w := httptest.NewRecorder()
	s.handleDocsList(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, `"docs":null`) {
		t.Fatalf("docs was serialized as null: %s", body)
	}
	if !strings.Contains(body, `"docs":[]`) {
		t.Fatalf("docs should be [], got: %s", body)
	}
}

func TestDocCommentsNeverSerializesThreadsOrRepliesAsNull(t *testing.T) {
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "doc001", Slug: "doc", Type: api.DocTypeMD}, nil)
	// Simulate fold returning nil Threads (same root cause class as BLOCKER-1).
	fv.threadsResp = &api.CommentsResponse{Doc: "doc001", Threads: nil}
	s, _ := newTestServerWithVault(t, fv)

	r := httptest.NewRequest(http.MethodGet, "/api/docs/doc001/comments", nil)
	r.SetPathValue("id", "doc001")
	w := httptest.NewRecorder()
	s.handleDocComments(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, `"threads":null`) {
		t.Fatalf("threads was serialized as null: %s", body)
	}
	if !strings.Contains(body, `"threads":[]`) {
		t.Fatalf("threads should be [], got: %s", body)
	}
}

func TestDocCommentsThreadRepliesNeverSerializesAsNull(t *testing.T) {
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "doc001", Slug: "doc", Type: api.DocTypeMD}, nil)
	// A thread that exists but has nil Replies -- exactly the "replies": null
	// scenario reported by the frontend.
	fv.threadsResp = &api.CommentsResponse{
		Doc: "doc001",
		Threads: []api.Thread{
			{Thread: "cabcde", Doc: "doc001", Status: api.StatusOpen, Body: "评论", Replies: nil},
		},
	}
	s, _ := newTestServerWithVault(t, fv)

	r := httptest.NewRequest(http.MethodGet, "/api/docs/doc001/comments", nil)
	r.SetPathValue("id", "doc001")
	w := httptest.NewRecorder()
	s.handleDocComments(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, `"replies":null`) {
		t.Fatalf("replies was serialized as null (BLOCKER-1 root-cause scenario): %s", body)
	}
	if !strings.Contains(body, `"replies":[]`) {
		t.Fatalf("replies should be [], got: %s", body)
	}
}

func TestDocHistoryNeverSerializesCommitsAsNull(t *testing.T) {
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "doc001", Slug: "doc", Type: api.DocTypeMD}, nil)
	// When opts.Vault has no Git (no git repo initialized), commits should
	// still be [], not null.
	s, _ := newTestServerWithVault(t, fv)

	r := httptest.NewRequest(http.MethodGet, "/api/docs/doc001/history", nil)
	r.SetPathValue("id", "doc001")
	w := httptest.NewRecorder()
	s.handleDocHistory(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, `"commits":null`) {
		t.Fatalf("commits was serialized as null: %s", body)
	}
	if !strings.Contains(body, `"commits":[]`) {
		t.Fatalf("commits should be [], got: %s", body)
	}
}
