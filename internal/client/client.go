// Package client is the HTTP client the CLI uses to talk to a running serve.
//
// Owner: W-core.
//
// Its reason for existing is design doc §6.4: while serve is running, it is
// the sole writer of the event log. So every CLI write command must first
// call lockfile.Probe — if a serve is detected, the write goes through this
// client instead; only when no serve is detected does the CLI fall back to
// holding the flock and writing directly. Read commands also prefer the API
// first, since it reflects the latest remap results.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/lockfile"
)

// Client points at a running serve instance.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// New builds a client. base looks like http://127.0.0.1:7777.
func New(base, token string) *Client {
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Timeout: 10 * time.Second},
	}
}

// FromServeInfo builds a client from a serve-detection result.
func FromServeInfo(info *lockfile.ServeInfo) *Client {
	return New(info.BaseURL(), info.Token)
}

// Detect probes whether a serve is available for the vault at root.
//
// Returning (nil, nil) means no serve is running and the caller should fall
// back to the direct-write path — this is not an error, and the CLI must not
// fail because of it. On a successful probe, Detect also calls /api/health
// once and checks that its Root matches the local vault; a mismatch (e.g.
// the port is actually held by some other vault's serve) also results in
// (nil, nil).
func Detect(ctx context.Context, root string) (*Client, error) {
	info, err := lockfile.Probe(root)
	if err != nil {
		return nil, nil
	}
	c := FromServeInfo(info)
	health, err := c.Health(ctx)
	if err != nil || health.Root != root {
		return nil, nil
	}
	return c, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		var apiErr api.ErrorResponse
		if jsonErr := json.Unmarshal(data, &apiErr); jsonErr == nil && apiErr.Error != "" {
			return &Error{Status: resp.StatusCode, Response: apiErr}
		}
		return fmt.Errorf("client: %s %s: unexpected status %d: %s", method, path, resp.StatusCode, string(data))
	}

	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Error is a structured wrapper around a 4xx/5xx response from serve, letting
// callers map api.Error* values to exit codes.
type Error struct {
	Status   int
	Response api.ErrorResponse
}

func (e *Error) Error() string {
	return fmt.Sprintf("client: %s (%s)", e.Response.Message, e.Response.Error)
}

// Health calls GET /api/health.
func (c *Client) Health(ctx context.Context) (*api.HealthResponse, error) {
	var out api.HealthResponse
	if err := c.do(ctx, http.MethodGet, "/api/health", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Docs calls GET /api/docs.
func (c *Client) Docs(ctx context.Context) (*api.DocsResponse, error) {
	var out api.DocsResponse
	if err := c.do(ctx, http.MethodGet, "/api/docs", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// NewDoc calls POST /api/docs.
func (c *Client) NewDoc(ctx context.Context, req api.NewDocRequest) (*api.NewDocResponse, error) {
	var out api.NewDocResponse
	if err := c.do(ctx, http.MethodPost, "/api/docs", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Comments calls GET /api/docs/{id}/comments. An empty docID means all
// documents, in which case the client first fetches /api/docs and then
// fetches and merges comments for each one.
func (c *Client) Comments(ctx context.Context, docID, status string) ([]api.Thread, error) {
	if docID == "" {
		docs, err := c.Docs(ctx)
		if err != nil {
			return nil, err
		}
		all := []api.Thread{} // never return nil here: callers marshal this straight to --json output
		for _, d := range docs.Docs {
			var out api.CommentsResponse
			if err := c.do(ctx, http.MethodGet, "/api/docs/"+url.PathEscape(d.ID)+"/comments", nil, &out); err != nil {
				return nil, err
			}
			for _, th := range out.Threads {
				if status != "" && th.Status != status {
					continue
				}
				all = append(all, th)
			}
		}
		return all, nil
	}

	var out api.CommentsResponse
	if err := c.do(ctx, http.MethodGet, "/api/docs/"+url.PathEscape(docID)+"/comments", nil, &out); err != nil {
		return nil, err
	}
	if out.Threads == nil {
		out.Threads = []api.Thread{}
	}
	if status == "" {
		return out.Threads, nil
	}
	filtered := []api.Thread{}
	for _, th := range out.Threads {
		if th.Status == status {
			filtered = append(filtered, th)
		}
	}
	return filtered, nil
}

// PostEvent calls POST /api/docs/{id}/events.
func (c *Client) PostEvent(ctx context.Context, docID string, req api.EventRequest) (*api.EventResponse, error) {
	var out api.EventResponse
	if err := c.do(ctx, http.MethodPost, "/api/docs/"+url.PathEscape(docID)+"/events", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FindThread locates, via serve, which document id a thread id belongs to.
func (c *Client) FindThread(ctx context.Context, threadRef string) (docID string, t *api.Thread, err error) {
	docs, err := c.Docs(ctx)
	if err != nil {
		return "", nil, err
	}
	var exactDoc string
	var exactThread *api.Thread
	var prefixDoc string
	var prefixThread *api.Thread
	ambiguous := false

	for _, d := range docs.Docs {
		var out api.CommentsResponse
		if err := c.do(ctx, http.MethodGet, "/api/docs/"+url.PathEscape(d.ID)+"/comments", nil, &out); err != nil {
			continue
		}
		for i := range out.Threads {
			th := &out.Threads[i]
			if th.Thread == threadRef {
				exactDoc, exactThread = d.ID, th
			} else if strings.HasPrefix(th.Thread, threadRef) {
				if prefixThread != nil {
					ambiguous = true
				}
				prefixDoc, prefixThread = d.ID, th
			}
		}
	}

	if exactThread != nil {
		return exactDoc, exactThread, nil
	}
	if prefixThread != nil && !ambiguous {
		return prefixDoc, prefixThread, nil
	}
	return "", nil, &Error{Status: http.StatusNotFound, Response: api.ErrorResponse{Error: api.ErrNotFound, Message: "thread not found"}}
}

// Compact calls POST /api/compact.
func (c *Client) Compact(ctx context.Context, req api.CompactRequest) (*api.CompactResponse, error) {
	var out api.CompactResponse
	if err := c.do(ctx, http.MethodPost, "/api/compact", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
