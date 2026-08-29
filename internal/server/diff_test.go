package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/gitx"
	"github.com/six-ddc/artx/internal/vault"
)

// diffRepo prepares a real git repository with one committed version of a
// document and returns the commit helper; tests then decide what the working
// copy / later commits look like. Skips when git isn't installed.
func diffRepo(t *testing.T, relPath string, v1 []byte) (dir string, repo *gitx.Repo, commit func(src []byte, msg string) string) {
	t.Helper()
	dir = t.TempDir()
	repo = gitx.Open(dir)
	if err := repo.Init(context.Background()); err != nil {
		t.Skip("git not available")
	}
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	commit = func(src []byte, msg string) string {
		if err := os.WriteFile(full, src, 0o644); err != nil {
			t.Fatal(err)
		}
		sha, err := repo.Commit(context.Background(), gitx.CommitOptions{Message: msg, Author: gitx.AuthorHuman})
		if err != nil || sha == "" {
			t.Fatalf("commit: sha=%q err=%v", sha, err)
		}
		return sha
	}
	commit(v1, "v1")
	sha, err := repo.HeadSHA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = sha
	return dir, repo, commit
}

func getDiff(t *testing.T, s *Server, id, query string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/docs/"+id+"/diff"+query, nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.handleDocDiff(w, r)
	return w
}

func decodeDiff(t *testing.T, w *httptest.ResponseRecorder) api.DiffResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp api.DiffResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestDocDiffMDAgainstWorkingCopy(t *testing.T) {
	v1 := []byte("---\ntitle: old\n---\n\n# Title\n\nDoomed paragraph.\n\nThe quick brown fox.\n")
	v2 := []byte("---\ntitle: new\n---\n\n# Title\n\nThe quick red fox.\n\nFresh paragraph.\n")
	dir, repo, _ := diffRepo(t, "doc/index.md", v1)

	fv := newFakeVault()
	a := &vault.Artifact{
		ID: "md0001", Slug: "doc", Type: api.DocTypeMD,
		Dir: filepath.Join(dir, "doc"), Path: filepath.Join(dir, "doc/index.md"), RelPath: "doc/index.md",
	}
	fv.put(a, v2) // ReadSource serves the working copy
	s, _ := newTestServerWithVault(t, fv)
	s.opts.Vault = &vault.Vault{Root: dir, Name: "test", Git: repo}

	sha, err := repo.HeadSHA(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	resp := decodeDiff(t, getDiff(t, s, "md0001", "?from="+sha))
	if resp.Doc != "md0001" || resp.Type != api.DocTypeMD || resp.From != sha || resp.To != "" {
		t.Fatalf("envelope = %+v", resp)
	}
	if !resp.FrontmatterChanged {
		t.Fatal("frontmatter_changed must be true")
	}
	if resp.Stats.Removed != 1 || resp.Stats.Added != 1 || resp.Stats.Modified != 1 {
		t.Fatalf("stats = %+v", resp.Stats)
	}
	if len(resp.Hunks) == 0 {
		t.Fatal("hunks must not be empty")
	}
	var removed *api.DiffBlock
	for i := range resp.Blocks {
		if resp.Blocks[i].Op == api.DiffRemoved {
			removed = &resp.Blocks[i]
		}
	}
	if removed == nil {
		t.Fatalf("no removed block in %+v", resp.Blocks)
	}
	// The removed block's HTML comes from a full render of the OLD version
	// and keeps its data-sourcepos for the frontend to rename.
	if !strings.Contains(removed.HTML, "Doomed paragraph.") || !strings.Contains(removed.HTML, "data-sourcepos") {
		t.Fatalf("removed html = %q", removed.HTML)
	}
}

// removedHTMLs collects op=removed blocks' html keyed by their old source
// slice, so tests can assert per-block fallbacks.
func removedHTMLs(t *testing.T, src []byte, blocks []api.DiffBlock) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, b := range blocks {
		if b.Op == api.DiffRemoved {
			out[string(src[b.From[0]:b.From[1]])] = b.HTML
		}
	}
	return out
}

func TestDocDiffMDRemovedUnrenderableBlocksFallback(t *testing.T) {
	// A raw HTML block renders without a data-sourcepos and a link reference
	// definition renders to nothing at all: neither can be pulled from the
	// old version's rendered HTML, so both must fall back to the escaped
	// source slice instead of coming back blank.
	v1 := []byte("Para one.\n\n<div class=\"raw\">raw block</div>\n\n[ref]: https://example.com \"Title\"\n\nPara two.\n")
	v2 := []byte("Para one.\n\nPara two.\n")
	dir, repo, _ := diffRepo(t, "doc/index.md", v1)
	sha, _ := repo.HeadSHA(context.Background())

	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "md0003", Slug: "doc", Type: api.DocTypeMD, Path: filepath.Join(dir, "doc/index.md"), RelPath: "doc/index.md"}, v2)
	s, _ := newTestServerWithVault(t, fv)
	s.opts.Vault = &vault.Vault{Root: dir, Name: "test", Git: repo}

	resp := decodeDiff(t, getDiff(t, s, "md0003", "?from="+sha))
	got := removedHTMLs(t, v1, resp.Blocks)
	if len(got) != 2 {
		t.Fatalf("removed blocks = %v, want 2", got)
	}
	for src, h := range got {
		if h == "" {
			t.Fatalf("removed block %q has empty html", src)
		}
		if !strings.HasPrefix(h, "<pre>") {
			t.Fatalf("removed block %q html = %q, want escaped-source fallback", src, h)
		}
	}
	if h := got["<div class=\"raw\">raw block</div>\n"]; !strings.Contains(h, "&lt;div") {
		t.Fatalf("raw html block must be escaped, got %q", h)
	}
}

func TestDocDiffMDRenderFailureFallback(t *testing.T) {
	// Broken frontmatter YAML makes renderer.Render fail on the old version;
	// every removed block must still carry the escaped-source fallback.
	v1 := []byte("---\n{[\n---\n\nDoomed paragraph.\n\nKept.\n")
	v2 := []byte("---\n{[\n---\n\nKept.\n")
	dir, repo, _ := diffRepo(t, "doc/index.md", v1)
	sha, _ := repo.HeadSHA(context.Background())

	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "md0004", Slug: "doc", Type: api.DocTypeMD, Path: filepath.Join(dir, "doc/index.md"), RelPath: "doc/index.md"}, v2)
	s, _ := newTestServerWithVault(t, fv)
	s.opts.Vault = &vault.Vault{Root: dir, Name: "test", Git: repo}

	resp := decodeDiff(t, getDiff(t, s, "md0004", "?from="+sha))
	got := removedHTMLs(t, v1, resp.Blocks)
	if h := got["Doomed paragraph."]; h != "<pre>Doomed paragraph.</pre>" {
		t.Fatalf("removed html = %q, want plain fallback", h)
	}
}

func TestDocDiffMDBetweenTwoRevisions(t *testing.T) {
	v1 := []byte("First.\n")
	v2 := []byte("First.\n\nSecond.\n")
	dir, repo, commit := diffRepo(t, "doc/index.md", v1)
	sha1, _ := repo.HeadSHA(context.Background())
	sha2 := commit(v2, "v2")

	fv := newFakeVault()
	a := &vault.Artifact{
		ID: "md0002", Slug: "doc", Type: api.DocTypeMD,
		Dir: filepath.Join(dir, "doc"), Path: filepath.Join(dir, "doc/index.md"), RelPath: "doc/index.md",
	}
	fv.put(a, []byte("working copy must not be consulted when to is given\n"))
	s, _ := newTestServerWithVault(t, fv)
	s.opts.Vault = &vault.Vault{Root: dir, Name: "test", Git: repo}

	resp := decodeDiff(t, getDiff(t, s, "md0002", "?from="+sha1+"&to="+sha2))
	if resp.To != sha2 {
		t.Fatalf("to = %q, want %q", resp.To, sha2)
	}
	if resp.Stats.Added != 1 || resp.Stats.Removed != 0 || resp.Stats.Modified != 0 {
		t.Fatalf("stats = %+v", resp.Stats)
	}
}

func TestDocDiffHTML(t *testing.T) {
	v1 := []byte(`<html><head><style>p{color:red}</style></head><body><p data-aid="e1">old</p><p data-aid="e2">gone</p></body></html>`)
	v2 := []byte(`<html><head><style>p{color:blue}</style></head><body><p data-aid="e1">new</p><p data-aid="e3">born</p></body></html>`)
	dir, repo, commit := diffRepo(t, "page/index.html", v1)
	sha1, _ := repo.HeadSHA(context.Background())
	sha2 := commit(v2, "v2")

	fv := newFakeVault()
	a := &vault.Artifact{
		ID: "html01", Slug: "page", Type: api.DocTypeHTML,
		Dir: filepath.Join(dir, "page"), Path: filepath.Join(dir, "page/index.html"), RelPath: "page/index.html",
	}
	fv.put(a, v2)
	s, _ := newTestServerWithVault(t, fv)
	s.opts.Vault = &vault.Vault{Root: dir, Name: "test", Git: repo}

	resp := decodeDiff(t, getDiff(t, s, "html01", "?from="+sha1+"&to="+sha2))
	if resp.Type != api.DocTypeHTML {
		t.Fatalf("type = %q", resp.Type)
	}
	if len(resp.Blocks) != 0 {
		t.Fatalf("html diff must carry no md blocks: %+v", resp.Blocks)
	}
	if !resp.ChromeChanged {
		t.Fatal("style change must set chrome_changed")
	}
	if resp.Stats.Modified != 1 || resp.Stats.Added != 1 || resp.Stats.Removed != 1 {
		t.Fatalf("stats = %+v", resp.Stats)
	}
	byOp := map[string]api.DiffElement{}
	for _, e := range resp.Elements {
		byOp[e.Op] = e
	}
	if byOp[api.DiffChanged].AID != "e1" || byOp[api.DiffAdded].AID != "e3" {
		t.Fatalf("elements = %+v", resp.Elements)
	}
	if rm := byOp[api.DiffRemoved]; rm.AID != "e2" || !strings.Contains(rm.HTML, "gone") {
		t.Fatalf("removed element = %+v", rm)
	}
}

func TestDocDiffErrors(t *testing.T) {
	// No git configured on the vault → 404, same contract as ?v= viewing.
	fv := newFakeVault()
	fv.put(&vault.Artifact{ID: "md0001", Slug: "doc", Type: api.DocTypeMD, RelPath: "doc/index.md"}, []byte("x\n"))
	s, _ := newTestServerWithVault(t, fv)

	if w := getDiff(t, s, "md0001", "?from=abc1234"); w.Code != http.StatusNotFound {
		t.Fatalf("no-git status = %d, want 404", w.Code)
	}
	if w := getDiff(t, s, "md0001", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("missing-from status = %d, want 400", w.Code)
	}
	if w := getDiff(t, s, "nosuch", "?from=abc1234"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown-doc status = %d, want 404", w.Code)
	}

	// A real repo but a bogus sha → 404.
	dir, repo, _ := diffRepo(t, "doc/index.md", []byte("x\n"))
	fv2 := newFakeVault()
	fv2.put(&vault.Artifact{ID: "md0002", Slug: "doc", Type: api.DocTypeMD, Path: filepath.Join(dir, "doc/index.md"), RelPath: "doc/index.md"}, []byte("x\n"))
	s2, _ := newTestServerWithVault(t, fv2)
	s2.opts.Vault = &vault.Vault{Root: dir, Name: "test", Git: repo}
	if w := getDiff(t, s2, "md0002", "?from=deadbee"); w.Code != http.StatusNotFound {
		t.Fatalf("bad-sha status = %d, want 404: %s", w.Code, w.Body.String())
	}
	var e api.ErrorResponse
	_ = json.Unmarshal(getDiff(t, s2, "md0002", "?from=deadbee").Body.Bytes(), &e)
	if e.Error != api.ErrNotFound {
		t.Fatalf("error code = %q, want not_found", e.Error)
	}
}
