// Package server implements artx serve: HTTP routing, SSE, auth, and
// single-writer serialization.
//
// Owned by: W-serve.
//
// Three invariants that must never be broken:
//  1. While serve is running it is the **sole writer** of the event log —
//     every write, including remap/orphan events emitted by the watcher, is
//     serialized through the writer goroutine. Once the CLI detects a running
//     serve, it routes writes through the API instead of touching the log
//     directly.
//  2. By default we only listen on 127.0.0.1; --host requires --token to also
//     be set, otherwise startup is refused.
//  3. We only read and write files inside the vault directory; every path
//     goes through vault.ResolvePath for traversal validation.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/eventlog"
	"github.com/six-ddc/artx/internal/lockfile"
	"github.com/six-ddc/artx/internal/remap"
	"github.com/six-ddc/artx/internal/render"
	"github.com/six-ddc/artx/internal/vault"
	"github.com/six-ddc/artx/internal/version"
	"github.com/six-ddc/artx/internal/watcher"
)

// Options configures a single serve run.
type Options struct {
	Vault *vault.Vault
	Host  string // defaults to 127.0.0.1
	Port  int    // defaults to 7777; 0 means auto-pick a free port
	Token string // required for non-local listeners
	Watch bool   // whether to enable the watcher
	Open  bool   // whether to open a browser after startup

	// Raw mirrors the CLI's --raw escape hatch: /raw/{id}/ is served without
	// the reviewer script injected. This is the mitigation from design doc
	// §12 risk analysis for heavy-JS html demos that misbehave inside the
	// sandboxed iframe.
	Raw bool
}

// ErrTokenRequired indicates --host is non-local but --token was not given.
var ErrTokenRequired = errTokenRequired

// Server is a running serve instance.
type Server struct {
	opts Options

	// vault is a narrow interface projection of opts.Vault, covering only
	// the methods handlers need. The sole reason it exists: opts.Vault is
	// frozen as the concrete type *vault.Vault, but during development that
	// package could still be a panicking skeleton, so an interface + fake
	// lets handlers be unit-tested independently.
	vault vaultFacade

	writer   *Writer
	hub      *Hub
	renderer *render.Renderer

	mu  sync.Mutex
	ln  net.Listener
	wch *watcher.Watcher

	// watchFailed records that the watcher failed to start, so /api/health
	// can report it accurately.
	watchFailed atomic.Bool
}

// vaultFacade is the minimal surface of vault.Vault that Server depends on;
// *vault.Vault satisfies it automatically.
type vaultFacade interface {
	Scan() ([]vault.Artifact, error)
	Lookup(ref string) (*vault.Artifact, error)
	New(slug, typ, title string) (*vault.Artifact, error)
	ReadSource(a *vault.Artifact) ([]byte, error)
	ResolvePath(rel string) (string, error)
	Docs(ctx context.Context, baseURL string) ([]api.Doc, error)
	Doc(ctx context.Context, a *vault.Artifact, baseURL string) (api.Doc, error)
	Threads(ctx context.Context, a *vault.Artifact) (*api.CommentsResponse, error)
	AllThreads(ctx context.Context, status string) ([]api.Thread, error)
	FindThread(ctx context.Context, threadRef string) (*vault.Artifact, *api.Thread, error)
	Author() string
}

// New validates options and builds a Server. It returns ErrTokenRequired if
// Host is not 127.0.0.1/localhost and Token is empty.
func New(opts Options) (*Server, error) {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	if opts.Port == 0 {
		opts.Port = 7777
		if opts.Vault != nil && opts.Vault.Cfg != nil && opts.Vault.Cfg.Port != 0 {
			opts.Port = opts.Vault.Cfg.Port
		}
	}
	if !isLoopbackHost(opts.Host) && opts.Token == "" {
		return nil, ErrTokenRequired
	}

	s := &Server{opts: opts, hub: NewHub(), renderer: render.New()}
	if opts.Vault != nil {
		s.vault = opts.Vault
		if opts.Vault.Store != nil {
			s.writer = NewWriter(opts.Vault.Store)
		}
	}
	return s, nil
}

func isLoopbackHost(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// Run starts listening and blocks until ctx is canceled.
//
// Startup sequence:
//  1. lockfile.AcquireServe claims .artx/serve.lock (errors out if a serve is
//     already running)
//  2. bind the port and write the actual port back to serve.lock
//  3. start the writer goroutine and the SSE hub
//  4. if Watch is set, start the watcher and run one ProcessAll pass to
//     repair drift that happened while serve was offline
//  5. http.Server.Serve
func (s *Server) Run(ctx context.Context) error {
	if s.opts.Vault == nil {
		return fmt.Errorf("server: vault required")
	}

	info := lockfile.ServeInfo{
		PID:       os.Getpid(),
		Host:      s.opts.Host,
		Port:      s.opts.Port,
		Token:     s.opts.Token,
		Root:      s.opts.Vault.Root,
		Version:   version.Version,
		Watch:     s.opts.Watch,
		StartedAt: time.Now(),
	}
	lock, err := lockfile.AcquireServe(s.opts.Vault.Root, info)
	if err != nil {
		return fmt.Errorf("server: acquire serve lock: %w", err)
	}
	defer lock.Unlock()

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.opts.Host, s.opts.Port))
	if err != nil {
		return fmt.Errorf("server: listen: %w", err)
	}
	defer ln.Close()

	s.mu.Lock()
	s.ln = ln
	if tcpAddr, ok := ln.Addr().(*net.TCPAddr); ok {
		s.opts.Port = tcpAddr.Port
	}
	s.mu.Unlock()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if s.writer != nil {
		go s.writer.Run(runCtx)
	}

	if s.opts.Watch {
		wch, err := watcher.New(watcher.Options{
			Vault:      s.opts.Vault,
			AutoCommit: true,
			InjectAID:  true,
			Remap:      remap.DefaultOptions(),
			Emit: func(n watcher.Notice) {
				name, data := FromNotice(n)
				s.hub.Broadcast(name, data)
			},
		})
		if err != nil {
			// Failing silently here would let /api/health lie about watch:true
			// while the whole remap pipeline is actually dead.
			s.watchFailed.Store(true)
			log.Printf("artx: watcher failed to start, auto-remap unavailable: %v", err)
		} else {
			s.mu.Lock()
			s.wch = wch
			s.mu.Unlock()
			go func() { _ = wch.Run(runCtx) }()
			go func() { _, _ = wch.ProcessAll(runCtx) }()
			log.Printf("artx: watcher started (vault %s)", s.opts.Vault.Root)
		}
	}

	// Request contexts must derive from runCtx: Shutdown never cancels
	// in-flight requests, so without this the endless SSE streams keep it
	// blocked until its timeout expires (~5s on every Ctrl-C).
	httpSrv := &http.Server{
		Handler:     s.Handler(),
		BaseContext: func(net.Listener) context.Context { return runCtx },
	}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()

	select {
	case <-ctx.Done():
		log.Printf("artx: shutting down")
		start := time.Now()
		shutdownCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel2()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			// Timed out waiting on connections that runCtx cancellation didn't
			// unblock; force-close them rather than leaking past our exit.
			log.Printf("artx: http shutdown incomplete after %s: %v", time.Since(start).Round(time.Millisecond), err)
			_ = httpSrv.Close()
		}
		httpDone := time.Now()
		s.mu.Lock()
		if s.wch != nil {
			_ = s.wch.Close()
		}
		s.mu.Unlock()
		log.Printf("artx: shutdown complete (http %s, watcher %s)",
			httpDone.Sub(start).Round(time.Millisecond),
			time.Since(httpDone).Round(time.Millisecond))
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// Addr returns the actual listening address, e.g. 127.0.0.1:7777.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	return fmt.Sprintf("%s:%d", s.opts.Host, s.opts.Port)
}

// BaseURL returns a clickable base URL for this server.
func (s *Server) BaseURL() string {
	host := s.opts.Host
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	port := s.opts.Port
	s.mu.Lock()
	if s.ln != nil {
		if tcpAddr, ok := s.ln.Addr().(*net.TCPAddr); ok {
			port = tcpAddr.Port
		}
	}
	s.mu.Unlock()
	return fmt.Sprintf("http://%s:%d", host, port)
}

// Handler assembles the full route table, letting tests exercise it directly
// without actually binding a port.
//
// Route table (mirrors blueprint.md's "HTTP API contract" one-to-one):
//
//	GET  /api/health
//	GET  /api/docs
//	POST /api/docs
//	GET  /api/docs/{id}
//	GET  /api/docs/{id}/raw
//	GET  /api/docs/{id}/history
//	GET  /api/docs/{id}/diff          version-to-version comparison (?from=sha[&to=sha])
//	GET  /api/docs/{id}/comments
//	POST /api/docs/{id}/events
//	POST /api/docs/{id}/element      (M2)
//	POST /api/docs/{id}/block        md block-level source editing
//	POST /api/compact
//	GET  /api/stream                  SSE
//	GET  /raw/{id}/                   html artifact entry point, reviewer script injected
//	GET  /raw/{id}/{path...}          artifact's sibling static assets
//	GET  /_artx/{path...}              embedded frontend assets (Vite base = /_artx/)
//	GET  /                            SPA shell
//	GET  /a/{id}                      SPA shell (client-side routing)
//	GET  /{path...}                   anything non-/api falls back to index.html
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/docs", s.handleDocsList)
	mux.HandleFunc("POST /api/docs", s.handleDocsCreate)
	mux.HandleFunc("GET /api/docs/{id}", s.handleDocDetail)
	mux.HandleFunc("GET /api/docs/{id}/raw", s.handleDocRawText)
	mux.HandleFunc("GET /api/docs/{id}/history", s.handleDocHistory)
	mux.HandleFunc("GET /api/docs/{id}/diff", s.handleDocDiff)
	mux.HandleFunc("GET /api/docs/{id}/comments", s.handleDocComments)
	mux.HandleFunc("POST /api/docs/{id}/events", s.handleDocEvents)
	mux.HandleFunc("POST /api/docs/{id}/element", s.handleDocElement)
	mux.HandleFunc("POST /api/docs/{id}/block", s.handleDocBlock)
	mux.HandleFunc("POST /api/compact", s.handleCompact)
	mux.HandleFunc("GET /api/stream", s.hub.ServeHTTP)
	mux.HandleFunc("GET /raw/{id}/{path...}", s.handleRaw)
	mux.Handle("GET /_artx/", s.artAssetsHandler())
	mux.HandleFunc("GET /", s.handleSPA)

	var h http.Handler = mux
	h = Auth(s.opts.Token, h)
	h = rejectTraversal(h)
	return h
}

// rejectTraversal blocks any path containing a ".." segment before it
// reaches the mux.
//
// Relying solely on the vault.ResolvePath check inside handleRaw isn't
// enough: http.ServeMux normalizes /raw/{id}/../../etc/passwd to
// /etc/passwd and issues a 301 before the request ever reaches a handler.
// The 301 itself doesn't leak any file, but the contract requires such
// requests to get a 404 (blueprint §8, W-serve acceptance criteria), and
// blocking traversal attempts at the outermost layer is more robust than
// relying on every downstream handler to do it correctly.
func rejectTraversal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, seg := range strings.Split(r.URL.Path, "/") {
			if seg == ".." {
				WriteError(w, http.StatusNotFound, api.ErrNotFound, "path traversal rejected")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// docURL builds a browser-clickable URL for a given document.
func (s *Server) docURL(id string) string {
	return s.BaseURL() + "/a/" + id
}

// writeErr maps an internal error to a uniform HTTP error response.
func (s *Server) writeErr(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, vault.ErrNotFound):
		WriteError(w, http.StatusNotFound, api.ErrNotFound, err.Error())
	case errors.Is(err, vault.ErrOutsideVault):
		WriteError(w, http.StatusNotFound, api.ErrNotFound, err.Error())
	case errors.Is(err, eventlog.ErrThreadNotFound):
		WriteError(w, http.StatusNotFound, api.ErrNotFound, err.Error())
	default:
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, err.Error())
	}
}

// WriteError writes a uniform error response body. code is one of the
// api.Err* constants.
func WriteError(w http.ResponseWriter, status int, code, msg string) {
	WriteJSON(w, status, api.ErrorResponse{Error: code, Message: msg})
}

// WriteJSON writes a JSON response.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// CSP is the Content-Security-Policy for the shell page.
// frame-src 'self' lets the sandboxed iframe load /raw/; same-origin
// isolation is otherwise guaranteed by the iframe's sandbox attribute.
const CSP = "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; " +
	"script-src 'self' 'unsafe-inline'; font-src 'self' data:; connect-src 'self'; frame-src 'self'"
