package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/watcher"
)

func TestFromNotice(t *testing.T) {
	n := watcher.Notice{Kind: "remap", DocID: "a7f3k2", Rev: "9f8e7d6", Remaps: 3, Orphans: 1}
	name, data := FromNotice(n)
	if name != api.SSEDoc {
		t.Fatalf("event name = %q, want %q", name, api.SSEDoc)
	}
	change, ok := data.(api.SSEDocChange)
	if !ok {
		t.Fatalf("wrong data type: %T", data)
	}
	if change.Doc != "a7f3k2" || change.Kind != "remap" || change.Rev != "9f8e7d6" || change.Remaps != 3 || change.Orphans != 1 {
		t.Fatalf("fields were not passed through correctly: %+v", change)
	}
}

// A slow subscriber must not stall Broadcast: once its channel is full, the
// message should just be dropped, and the write path must not block.
func TestHubBroadcastDoesNotBlockOnSlowSubscriber(t *testing.T) {
	h := NewHub()
	slow := h.subscribe()
	defer h.unsubscribe(slow)

	// Flood the slow subscriber's buffer (nobody is reading slow.ch).
	for i := 0; i < subscriberBuffer+10; i++ {
		done := make(chan struct{})
		go func() {
			h.Broadcast(api.SSEPing, struct{}{})
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("Broadcast was blocked by the slow subscriber on call %d", i)
		}
	}
}

// flushRecorder wraps httptest.ResponseRecorder with http.Flusher support,
// recording the written body in a concurrency-safe way so tests can poll it
// to check SSE output.
type flushRecorder struct {
	*httptest.ResponseRecorder
	mu   sync.Mutex
	body strings.Builder
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (f *flushRecorder) Write(b []byte) (int, error) {
	f.mu.Lock()
	f.body.Write(b)
	f.mu.Unlock()
	return f.ResponseRecorder.Write(b)
}

func (f *flushRecorder) Flush() {}

func (f *flushRecorder) snapshot() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.body.String()
}

func TestHubServeHTTPStreamsEvents(t *testing.T) {
	h := NewHub()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/stream", nil).WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for h.subscriberCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscription was not established")
		}
		time.Sleep(5 * time.Millisecond)
	}

	h.Broadcast(api.SSEComments, api.SSEComment{Doc: "a1", Threads: []string{"c1"}})

	deadline = time.Now().Add(2 * time.Second)
	for {
		body := rec.snapshot()
		if strings.Contains(body, "event: comments") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("did not receive SSE message within the deadline, body=%q", body)
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeHTTP did not exit after context cancellation")
	}
}
