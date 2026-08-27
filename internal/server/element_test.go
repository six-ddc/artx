package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/vault"
)

// M2 gap: POST /api/docs/{id}/element (blueprint §11). Not on the required
// acceptance test list, but htmlaid now has a real implementation, so add a
// smoke test confirming the write-back and response format are correct.
func TestHandleDocElementReplacesAndWritesBack(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "index.html")
	src := []byte(`<html><body><div data-aid="b2c9x1">old</div></body></html>`)
	if err := os.WriteFile(htmlPath, src, 0o644); err != nil {
		t.Fatal(err)
	}

	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "html01", Slug: "demo", Type: api.DocTypeHTML, Dir: dir, Path: htmlPath}, src)
	s, _ := newTestServerWithVault(t, fv)

	body, _ := json.Marshal(map[string]string{"aid": "b2c9x1", "html": "new content"})
	r := httptest.NewRequest(http.MethodPost, "/api/docs/html01/element", bytes.NewReader(body))
	r.SetPathValue("id", "html01")
	w := httptest.NewRecorder()
	s.handleDocElement(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	out, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("new content")) {
		t.Fatalf("new content was not written back to the source file: %s", out)
	}
	if bytes.Contains(out, []byte("old")) {
		t.Fatalf("old content was not replaced: %s", out)
	}
}

func TestHandleDocElementRejectsHistoricalVersion(t *testing.T) {
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "html01", Slug: "demo", Type: api.DocTypeHTML, Dir: t.TempDir()}, nil)
	s, _ := newTestServerWithVault(t, fv)

	body, _ := json.Marshal(map[string]string{"aid": "b2c9x1", "html": "x"})
	r := httptest.NewRequest(http.MethodPost, "/api/docs/html01/element?v=deadbeef", bytes.NewReader(body))
	r.SetPathValue("id", "html01")
	w := httptest.NewRecorder()
	s.handleDocElement(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("editing an element on a historical version should be 409, got %d", w.Code)
	}
}
