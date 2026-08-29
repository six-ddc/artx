package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/six-ddc/artx/internal/anchor"
	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/docdiff"
	"github.com/six-ddc/artx/internal/eventlog"
	"github.com/six-ddc/artx/internal/gitx"
	"github.com/six-ddc/artx/internal/htmlaid"
	"github.com/six-ddc/artx/internal/idgen"
	"github.com/six-ddc/artx/internal/mdsrc"
	"github.com/six-ddc/artx/internal/version"
)

// These can be overridden in tests to work around idgen not yet being
// implemented (it panics during the skeleton phase). The production code
// path keeps calling the real idgen, so nothing here needs to change once
// that package is finished.
var (
	genThreadID = idgen.ThreadID
	genReplyID  = idgen.ReplyID
	genEventID  = idgen.EventID
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	var root, name string
	watch := s.opts.Watch && !s.watchFailed.Load()
	if s.opts.Vault != nil {
		root, name = s.opts.Vault.Root, s.opts.Vault.Name
	}
	WriteJSON(w, http.StatusOK, api.HealthResponse{
		OK: "ok", Version: version.Version, Vault: name, Root: root, Watch: watch,
	})
}

func (s *Server) handleDocsList(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "vault unavailable")
		return
	}
	docs, err := s.vault.Docs(r.Context(), s.BaseURL())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	// api.DocsResponse.Docs has no omitempty: a nil slice would serialize as
	// "docs":null, and the frontend crashes when it expects an array (same
	// class of bug as Threads/Replies in handleDocComments below).
	if docs == nil {
		docs = []api.Doc{}
	}
	var root, name string
	if s.opts.Vault != nil {
		root, name = s.opts.Vault.Root, s.opts.Vault.Name
	}
	WriteJSON(w, http.StatusOK, api.DocsResponse{Vault: name, Root: root, Docs: docs})
}

func (s *Server) handleDocsCreate(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "vault unavailable")
		return
	}
	var req api.NewDocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "invalid json body")
		return
	}
	if req.Slug == "" || (req.Type != api.DocTypeMD && req.Type != api.DocTypeHTML) {
		WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "slug and a valid type are required")
		return
	}
	a, err := s.vault.New(req.Slug, req.Type, req.Title)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.hub.Broadcast(api.SSEDocs, struct{}{})
	WriteJSON(w, http.StatusOK, api.NewDocResponse{
		ID: a.ID, Path: a.Path, URL: s.docURL(a.ID), Slug: a.Slug, Type: a.Type,
	})
}

func (s *Server) handleDocDetail(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "vault unavailable")
		return
	}
	id := r.PathValue("id")
	a, err := s.vault.Lookup(id)
	if err != nil {
		s.writeErr(w, err)
		return
	}

	rev := r.URL.Query().Get("v")
	var src []byte
	if rev != "" {
		if s.opts.Vault == nil || s.opts.Vault.Git == nil {
			WriteError(w, http.StatusNotFound, api.ErrNotFound, "no git history available")
			return
		}
		src, err = s.opts.Vault.Git.ShowFile(r.Context(), rev, a.RelPath)
	} else {
		src, err = s.vault.ReadSource(a)
	}
	if err != nil {
		s.writeErr(w, err)
		return
	}

	doc, err := s.vault.Doc(r.Context(), a, s.BaseURL())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	detail := api.DocDetail{Doc: doc, Rev0: rev}

	switch a.Type {
	case api.DocTypeMD:
		res, err := s.renderer.Render(src)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, api.ErrInternal, err.Error())
			return
		}
		detail.HTML = res.HTML
		detail.BodyOffset = res.BodyOffset
		detail.Frontmatter = res.Frontmatter
		detail.HasMermaid = res.HasMermaid
		detail.HasMath = res.HasMath
		if detail.Title == "" {
			detail.Title = res.Title
		}
	case api.DocTypeHTML:
		detail.RawURL = "/raw/" + a.ID + "/"
	}
	WriteJSON(w, http.StatusOK, detail)
}

func (s *Server) handleDocRawText(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "vault unavailable")
		return
	}
	id := r.PathValue("id")
	a, err := s.vault.Lookup(id)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	src, err := s.vault.ReadSource(a)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(src)
}

func (s *Server) handleDocHistory(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "vault unavailable")
		return
	}
	id := r.PathValue("id")
	a, err := s.vault.Lookup(id)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	var commits []gitx.Commit
	if s.opts.Vault != nil && s.opts.Vault.Git != nil {
		commits, _ = s.opts.Vault.Git.LogFile(r.Context(), a.RelPath, 50)
	}
	if commits == nil {
		commits = []gitx.Commit{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"commits": commits})
}

// handleDocDiff implements GET /api/docs/{id}/diff?from=<sha>[&to=<sha>]:
// a version-to-version comparison. from is required; an omitted to means the
// working copy, so the endpoint covers both "historical vs current" and
// "historical vs historical" with one shape. The heavy lifting lives in
// internal/docdiff; this handler only fetches the two sources, fills each
// removed md block's HTML from a full render of the OLD version, and
// assembles the response.
func (s *Server) handleDocDiff(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "vault unavailable")
		return
	}
	id := r.PathValue("id")
	a, err := s.vault.Lookup(id)
	if err != nil {
		s.writeErr(w, err)
		return
	}

	from := r.URL.Query().Get("from")
	if from == "" {
		WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "from is required")
		return
	}
	to := r.URL.Query().Get("to")
	if s.opts.Vault == nil || s.opts.Vault.Git == nil {
		WriteError(w, http.StatusNotFound, api.ErrNotFound, "no git history available")
		return
	}
	git := s.opts.Vault.Git

	// A missing repo and an unknown sha both surface here as errors; either
	// way the requested version doesn't exist.
	srcA, err := git.ShowFile(r.Context(), from, a.RelPath)
	if err != nil {
		WriteError(w, http.StatusNotFound, api.ErrNotFound, "revision not found: "+from)
		return
	}
	var srcB []byte
	if to != "" {
		srcB, err = git.ShowFile(r.Context(), to, a.RelPath)
		if err != nil {
			WriteError(w, http.StatusNotFound, api.ErrNotFound, "revision not found: "+to)
			return
		}
	} else {
		srcB, err = s.vault.ReadSource(a)
		if err != nil {
			s.writeErr(w, err)
			return
		}
	}

	resp := api.DiffResponse{
		Doc: a.ID, Type: a.Type, From: from, To: to,
		Hunks: docdiff.Unified(srcA, srcB),
	}

	switch a.Type {
	case api.DocTypeMD:
		blocks, stats := docdiff.BlockOps(srcA, srcB)
		// BlockOps only covers the body; a frontmatter-only edit is reported
		// through this flag (the hunks naturally cover the whole file).
		fmA, _ := mdsrc.SplitFrontmatter(srcA)
		fmB, _ := mdsrc.SplitFrontmatter(srcB)
		resp.FrontmatterChanged = !bytes.Equal(fmA, fmB)
		if stats.Removed > 0 {
			var idx map[string]string
			if res, rerr := s.renderer.Render(srcA); rerr == nil {
				idx = docdiff.TopLevelBySourcepos(res.HTML)
			}
			for i := range blocks {
				if blocks[i].Op != api.DiffRemoved || len(blocks[i].From) != 2 {
					continue
				}
				if h := idx[fmt.Sprintf("%d:%d", blocks[i].From[0], blocks[i].From[1])]; h != "" {
					blocks[i].HTML = h
					continue
				}
				// The index has gaps: goldmark emits raw HTML blocks without a
				// data-sourcepos and link reference definitions without any
				// node at all, and a failed render of the old version loses
				// the whole index. Deleted content must never come back blank,
				// so fall back to the escaped source slice.
				lo, hi := blocks[i].From[0], blocks[i].From[1]
				if lo >= 0 && hi <= len(srcA) && lo < hi {
					blocks[i].HTML = "<pre>" + stdhtml.EscapeString(string(srcA[lo:hi])) + "</pre>"
				}
			}
		}
		resp.Blocks = blocks
		resp.Stats = stats
	case api.DocTypeHTML:
		elems, chrome, derr := docdiff.ElementOps(srcA, srcB)
		if derr != nil {
			WriteError(w, http.StatusInternalServerError, api.ErrInternal, derr.Error())
			return
		}
		resp.Elements = elems
		resp.ChromeChanged = chrome
		for _, e := range elems {
			switch e.Op {
			case api.DiffAdded:
				resp.Stats.Added++
			case api.DiffRemoved:
				resp.Stats.Removed++
			case api.DiffChanged:
				resp.Stats.Modified++
			}
		}
	}
	WriteJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDocComments(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "vault unavailable")
		return
	}
	id := r.PathValue("id")
	a, err := s.vault.Lookup(id)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	resp, err := s.vault.Threads(r.Context(), a)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	normalizeCommentsResponse(resp)
	WriteJSON(w, http.StatusOK, resp)
}

// normalizeCommentsResponse guarantees that array-shaped contract fields
// never serialize as JSON null: neither api.CommentsResponse.Threads nor
// api.Thread.Replies has omitempty, so a nil slice would become
// "threads":null / "replies":null and crash the whole page wherever the
// frontend expects an array (BLOCKER-1). The root cause lives in
// eventlog.Fold's folding logic (being fixed by W-core); this is the last
// line of defense at the HTTP response boundary, independent of whether the
// upstream fix has landed yet.
func normalizeCommentsResponse(resp *api.CommentsResponse) {
	if resp == nil {
		return
	}
	if resp.Threads == nil {
		resp.Threads = []api.Thread{}
	}
	for i := range resp.Threads {
		if resp.Threads[i].Replies == nil {
			resp.Threads[i].Replies = []api.Reply{}
		}
	}
}

// resolveAuthor implements the identity rules from design doc §13 decision 2
// and blueprint.md §5.2: in local mode (no token) we take vault.Author()
// ($ARTX_AUTHOR/$USER) unless the request explicitly supplies an author; in
// --token mode the token itself is the identity, and the request body's
// author is just a self-reported display name, always wrapped as
// "artx-web <display name>" with "reviewer" as the default display name.
func (s *Server) resolveAuthor(reqAuthor string) string {
	if s.opts.Token != "" {
		name := reqAuthor
		if name == "" {
			name = "reviewer"
		}
		return "artx-web " + name
	}
	if reqAuthor != "" {
		return reqAuthor
	}
	if s.vault != nil {
		return s.vault.Author()
	}
	return ""
}

// threadOfComment extracts the owning thread id from a comment id (either a
// thread id or a reply id). An id always has the shape "<thread>" or
// "<thread>.<3 digits>" (the format idgen.ReplyID produces), so splitting on
// the first "." is sufficient without depending on idgen (which panics
// during the skeleton phase).
func threadOfComment(id string) string {
	if i := strings.IndexByte(id, '.'); i >= 0 {
		return id[:i]
	}
	return id
}

func (s *Server) handleDocEvents(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil || s.writer == nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "server not ready")
		return
	}
	id := r.PathValue("id")
	if r.URL.Query().Get("v") != "" {
		WriteError(w, http.StatusConflict, api.ErrConflict, "cannot write to a historical version")
		return
	}
	a, err := s.vault.Lookup(id)
	if err != nil {
		s.writeErr(w, err)
		return
	}

	var req api.EventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "invalid json body")
		return
	}

	var ev eventlog.Event
	var threadOut, statusOut string

	switch req.Type {
	case eventlog.KindCreate:
		if req.Body == "" {
			WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "body is required")
			return
		}
		var anc anchor.Anchor
		switch a.Type {
		case api.DocTypeMD:
			if req.Selection == nil {
				WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "selection is required for md documents")
				return
			}
			src, err := s.vault.ReadSource(a)
			if err != nil {
				s.writeErr(w, err)
				return
			}
			doc, err := mdsrc.Parse(src)
			if err != nil {
				WriteError(w, http.StatusInternalServerError, api.ErrInternal, err.Error())
				return
			}
			anc = s.safeFromSelection(doc, *req.Selection)
		case api.DocTypeHTML:
			if req.Element == nil {
				WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "element is required for html documents")
				return
			}
			anc = s.safeFromElement(*req.Element)
		default:
			WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "unsupported document type")
			return
		}
		if s.opts.Vault != nil && s.opts.Vault.Git != nil {
			if rev, err := s.opts.Vault.Git.HeadSHA(r.Context()); err == nil {
				anc.Rev = rev
			}
		}
		threadID := genThreadID()
		ev = eventlog.Event{
			E: eventlog.KindCreate, Thread: threadID,
			Author: s.resolveAuthor(req.Author), Body: req.Body, Anchor: &anc,
		}
		threadOut, statusOut = threadID, api.StatusOpen

	case eventlog.KindReply:
		if req.Thread == "" || req.Body == "" {
			WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "thread and body are required")
			return
		}
		ev = eventlog.Event{
			E: eventlog.KindReply, Thread: req.Thread, ID: genReplyID(req.Thread),
			Author: s.resolveAuthor(req.Author), Body: req.Body,
		}
		threadOut = req.Thread

	case eventlog.KindEdit:
		if req.Target == "" || req.Body == "" {
			WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "target and body are required")
			return
		}
		ev = eventlog.Event{
			E: eventlog.KindEdit, Target: req.Target,
			Author: s.resolveAuthor(req.Author), Body: req.Body,
		}
		threadOut = threadOfComment(req.Target)

	case eventlog.KindAddressed:
		if req.Thread == "" {
			WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "thread is required")
			return
		}
		ev = eventlog.Event{
			E: eventlog.KindAddressed, Thread: req.Thread,
			By: s.resolveAuthor(req.Author), Commit: req.Commit, Note: req.Note,
		}
		threadOut, statusOut = req.Thread, api.StatusAddressed

	case eventlog.KindResolve:
		if req.Thread == "" {
			WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "thread is required")
			return
		}
		ev = eventlog.Event{E: eventlog.KindResolve, Thread: req.Thread, By: s.resolveAuthor(req.Author)}
		threadOut, statusOut = req.Thread, api.StatusResolved

	case eventlog.KindReopen:
		if req.Thread == "" {
			WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "thread is required")
			return
		}
		ev = eventlog.Event{
			E: eventlog.KindReopen, Thread: req.Thread,
			By: s.resolveAuthor(req.Author), Note: req.Note,
		}
		threadOut, statusOut = req.Thread, api.StatusOpen

	case eventlog.KindDelete:
		if req.Thread == "" {
			WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "thread is required")
			return
		}
		ev = eventlog.Event{E: eventlog.KindDelete, Thread: req.Thread, By: s.resolveAuthor(req.Author)}
		threadOut = req.Thread

	default:
		WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "unknown event type: "+req.Type)
		return
	}

	ev.EID = genEventID()
	ev.TS = time.Now()

	if err := s.writer.Append(r.Context(), id, ev); err != nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, err.Error())
		return
	}

	s.hub.Broadcast(api.SSEComments, api.SSEComment{Doc: id, Threads: []string{threadOut}})

	WriteJSON(w, http.StatusOK, api.EventResponse{
		OK: "ok", Thread: threadOut, EventID: ev.EID, Status: statusOut,
	})
}

// handleDocBlock is the md counterpart of handleDocElement: block-level
// source editing. The client addresses a block by its data-sourcepos byte
// range and edits the SOURCE slice (never a rendered-HTML round-trip, which
// cannot be mapped back to markdown losslessly). Freshness is proven by
// echoing the original slice: a mismatch means the file changed since the
// client rendered it — 409, reload and retry.
func (s *Server) handleDocBlock(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "vault unavailable")
		return
	}
	id := r.PathValue("id")
	if r.URL.Query().Get("v") != "" {
		WriteError(w, http.StatusConflict, api.ErrConflict, "cannot write to a historical version")
		return
	}
	a, err := s.vault.Lookup(id)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	if a.Type != api.DocTypeMD {
		WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "block edits only apply to md documents")
		return
	}

	var req struct {
		Start    int    `json:"start"`
		End      int    `json:"end"`
		Original string `json:"original"`
		Content  string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "invalid json body")
		return
	}

	src, err := s.vault.ReadSource(a)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	if req.Start < 0 || req.End < req.Start || req.End > len(src) {
		WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "block range out of bounds")
		return
	}
	if string(src[req.Start:req.End]) != req.Original {
		WriteError(w, http.StatusConflict, api.ErrConflict, "document changed since it was rendered; reload and retry")
		return
	}

	out := make([]byte, 0, len(src)-(req.End-req.Start)+len(req.Content))
	out = append(out, src[:req.Start]...)
	out = append(out, req.Content...)
	out = append(out, src[req.End:]...)
	if err := os.WriteFile(a.Path, out, 0o644); err != nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, err.Error())
		return
	}

	var commitSHA string
	if s.opts.Vault != nil && s.opts.Vault.Git != nil {
		commitSHA, _ = s.opts.Vault.Git.Commit(r.Context(), gitx.CommitOptions{
			Message: fmt.Sprintf("artx: edit block in %s", a.Slug),
			Author:  gitx.AuthorHuman,
			Paths:   []string{a.RelPath},
		})
	}
	s.hub.Broadcast(api.SSEDoc, api.SSEDocChange{Doc: id, Kind: "content", Rev: commitSHA})
	WriteJSON(w, http.StatusOK, map[string]any{"ok": "ok", "commit": commitSHA})
}

// handleDocElement implements the M2 milestone: in-browser direct editing of
// html elements (blueprint §11).
func (s *Server) handleDocElement(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "vault unavailable")
		return
	}
	id := r.PathValue("id")
	if r.URL.Query().Get("v") != "" {
		WriteError(w, http.StatusConflict, api.ErrConflict, "cannot write to a historical version")
		return
	}
	a, err := s.vault.Lookup(id)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	if a.Type != api.DocTypeHTML {
		WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "element edits only apply to html documents")
		return
	}

	var req struct {
		AID    string `json:"aid"`
		HTML   string `json:"html"`
		Author string `json:"author,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "invalid json body")
		return
	}
	if req.AID == "" {
		WriteError(w, http.StatusBadRequest, api.ErrBadRequest, "aid is required")
		return
	}

	src, err := s.vault.ReadSource(a)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	out, err := htmlaid.ReplaceElementHTML(src, req.AID, []byte(req.HTML))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, err.Error())
		return
	}
	if err := os.WriteFile(a.Path, out, 0o644); err != nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, err.Error())
		return
	}

	var commitSHA string
	if s.opts.Vault != nil && s.opts.Vault.Git != nil {
		commitSHA, _ = s.opts.Vault.Git.Commit(r.Context(), gitx.CommitOptions{
			Message: fmt.Sprintf("artx: edit element %s in %s", req.AID, a.Slug),
			Author:  gitx.AuthorHuman,
			Paths:   []string{a.RelPath},
		})
	}
	s.hub.Broadcast(api.SSEDoc, api.SSEDocChange{Doc: id, Kind: "content", Rev: commitSHA})
	WriteJSON(w, http.StatusOK, map[string]any{"ok": "ok", "commit": commitSHA})
}

func (s *Server) handleCompact(w http.ResponseWriter, r *http.Request) {
	if s.opts.Vault == nil || s.opts.Vault.Store == nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "vault store unavailable")
		return
	}
	var req api.CompactRequest
	if r.Body != nil && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	store := s.opts.Vault.Store
	ids := []string{req.Doc}
	if req.Doc == "" {
		var err error
		ids, err = store.DocIDs()
		if err != nil {
			s.writeErr(w, err)
			return
		}
	}

	stats := make([]api.CompactStat, 0, len(ids))
	for _, docID := range ids {
		stat, err := store.Compact(docID, eventlog.CompactOptions{Force: req.Force})
		if err != nil {
			s.writeErr(w, err)
			return
		}
		stats = append(stats, stat)
	}

	var commitSHA string
	if s.opts.Vault.Git != nil {
		commitSHA, _ = s.opts.Vault.Git.Commit(r.Context(), gitx.CommitOptions{
			Message: eventlog.CompactMessage(stats), Author: gitx.AuthorArtx,
		})
	}
	s.hub.Broadcast(api.SSEDocs, struct{}{})
	WriteJSON(w, http.StatusOK, api.CompactResponse{Stats: stats, Commit: commitSHA})
}

// handleRaw implements GET /raw/{id}/ and GET /raw/{id}/{path...}: when path
// is empty this is the html artifact entry point (reviewer script injected),
// otherwise it serves a sibling static asset. Every relative path is
// validated by resolveWithin for traversal (design doc §9: serve only reads
// and writes files inside the vault directory).
func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "vault unavailable")
		return
	}
	id := r.PathValue("id")
	rel := r.PathValue("path")

	a, err := s.vault.Lookup(id)
	if err != nil {
		WriteError(w, http.StatusNotFound, api.ErrNotFound, "not found")
		return
	}
	if a.Type != api.DocTypeHTML {
		WriteError(w, http.StatusNotFound, api.ErrNotFound, "not an html artifact")
		return
	}

	if rel == "" {
		src, err := s.vault.ReadSource(a)
		if err != nil {
			WriteError(w, http.StatusNotFound, api.ErrNotFound, "not found")
			return
		}
		out, err := s.injectReviewer(src)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, api.ErrInternal, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The artifact is live-edited on disk; a heuristically cached copy
		// (this response carries no validators) would pin the iframe to a
		// stale version across reloads.
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)
		return
	}

	full, ok := resolveWithin(a.Dir, rel)
	if !ok {
		WriteError(w, http.StatusNotFound, api.ErrNotFound, "not found")
		return
	}
	fi, err := os.Stat(full)
	if err != nil || fi.IsDir() {
		WriteError(w, http.StatusNotFound, api.ErrNotFound, "not found")
		return
	}
	http.ServeFile(w, r, full)
}

// resolveWithin resolves the relative path rel under root into an absolute
// path, rejecting any attempt to traverse outside root. Callers must treat
// ok=false as "not found" (404) and must not leak the actual reason — this
// is a standalone fallback implementation for /raw static assets, used
// before vault.ResolvePath is ready.
func resolveWithin(root, rel string) (string, bool) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	// Prepend a virtual root before Clean: any ".." can only push the result
	// up to the virtual root, never past it.
	cleaned := filepath.Clean(string(filepath.Separator) + rel)
	full := filepath.Join(rootAbs, cleaned)
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", false
	}
	if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootAbs+string(filepath.Separator)) {
		return "", false
	}
	return fullAbs, true
}

func (s *Server) artAssetsHandler() http.Handler {
	fsys, err := DistFS()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			WriteError(w, http.StatusInternalServerError, api.ErrInternal, "dist assets unavailable")
		})
	}
	fileServer := http.StripPrefix("/_artx/", http.FileServer(http.FS(fsys)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/_artx/assets/") {
			// Vite content-hashed filenames: any change gets a new URL, so
			// these are safe to cache forever.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// Fixed-name files (reviewer.js): embed.FS responses carry no
			// Last-Modified/ETag, so browsers heuristically cache them and
			// keep running a stale bundle across artx upgrades. no-cache
			// forces a refetch per load — these files are tiny.
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// handleSPA is the fallback for any path that isn't /api, /raw, or /_art:
// it always returns index.html so TanStack Router's client-side routing
// takes over (design doc §8).
func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	fsys, err := DistFS()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "dist assets unavailable")
		return
	}
	f, err := fsys.Open("index.html")
	if err != nil {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "index.html missing")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", CSP)
	// The shell must never be heuristically cached (no validators on
	// embed.FS responses): a stale index.html keeps referencing old hashed
	// bundles and every frontend fix silently fails to reach the browser.
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}
