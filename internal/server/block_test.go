package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/vault"
)

const blockSrc = "---\naid: md0001\n---\n\n# 标题\n\n第一段。\n\n第二段。\n"

func newBlockDoc(t *testing.T) (*fakeVault, string) {
	t.Helper()
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "index.md")
	if err := os.WriteFile(mdPath, []byte(blockSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "md0001", Slug: "demo", Type: api.DocTypeMD, Dir: dir, Path: mdPath}, []byte(blockSrc))
	return fv, mdPath
}

func postBlock(t *testing.T, s *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/docs/md0001/block", bytes.NewReader(raw))
	r.SetPathValue("id", "md0001")
	w := httptest.NewRecorder()
	s.handleDocBlock(w, r)
	return w
}

func TestHandleDocBlockReplacesSourceSlice(t *testing.T) {
	fv, mdPath := newBlockDoc(t)
	s, _ := newTestServerWithVault(t, fv)

	original := "第一段。"
	start := bytes.Index([]byte(blockSrc), []byte(original))
	w := postBlock(t, s, map[string]any{
		"start": start, "end": start + len(original),
		"original": original, "content": "改写后的第一段。",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	out, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\naid: md0001\n---\n\n# 标题\n\n改写后的第一段。\n\n第二段。\n"
	if string(out) != want {
		t.Fatalf("file after edit = %q, want %q", out, want)
	}
}

func TestHandleDocBlockConflictsOnStaleOriginal(t *testing.T) {
	fv, mdPath := newBlockDoc(t)
	s, _ := newTestServerWithVault(t, fv)

	original := "第一段。"
	start := bytes.Index([]byte(blockSrc), []byte(original))
	w := postBlock(t, s, map[string]any{
		"start": start, "end": start + len(original),
		"original": "已经不是这段了", "content": "x",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("stale original should be 409, got %d: %s", w.Code, w.Body.String())
	}
	out, _ := os.ReadFile(mdPath)
	if string(out) != blockSrc {
		t.Fatalf("a conflicting edit must not touch the file: %q", out)
	}
}

func TestHandleDocBlockRejectsOutOfBoundsAndHTML(t *testing.T) {
	fv, _ := newBlockDoc(t)
	s, _ := newTestServerWithVault(t, fv)

	if w := postBlock(t, s, map[string]any{"start": 5, "end": 99999, "original": "x", "content": "y"}); w.Code != http.StatusBadRequest {
		t.Fatalf("out-of-bounds range should be 400, got %d", w.Code)
	}

	htmlDir := t.TempDir()
	fv.put(&vault.Artifact{ID: "html01", Slug: "h", Type: api.DocTypeHTML, Dir: htmlDir}, nil)
	raw, _ := json.Marshal(map[string]any{"start": 0, "end": 0, "original": "", "content": "x"})
	r := httptest.NewRequest(http.MethodPost, "/api/docs/html01/block", bytes.NewReader(raw))
	r.SetPathValue("id", "html01")
	w := httptest.NewRecorder()
	s.handleDocBlock(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("block edit on an html doc should be 400, got %d", w.Code)
	}
}
