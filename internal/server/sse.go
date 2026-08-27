package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/watcher"
)

// PingInterval is the SSE heartbeat interval.
const PingInterval = 25 * time.Second

// subscriberBuffer is the size of each subscriber's pending-message buffer.
// Once full, messages are dropped — a slow consumer must never be allowed to
// block Broadcast (blueprint.md §5.4 backpressure ruling).
const subscriberBuffer = 32

type sseMsg struct {
	event string
	data  any
}

type subscriber struct {
	ch chan sseMsg
}

// Hub broadcasts SSE events to all subscribers.
type Hub struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

// NewHub creates a hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[*subscriber]struct{})}
}

// Broadcast pushes one event to every subscriber. name is one of the
// api.SSE* constants, and data is JSON-encoded. When a subscriber's channel
// is full, **that subscriber's message is dropped** rather than blocking —
// the frontend catches up on the next invalidate or reconnect, so a slow
// client must never be allowed to stall the write path.
func (h *Hub) Broadcast(name string, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		select {
		case sub.ch <- sseMsg{event: name, data: data}:
		default:
			// Slow subscriber: drop this message, never block the write path.
		}
	}
}

func (h *Hub) subscribe() *subscriber {
	sub := &subscriber{ch: make(chan sseMsg, subscriberBuffer)}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

func (h *Hub) unsubscribe(sub *subscriber) {
	h.mu.Lock()
	delete(h.subs, sub)
	h.mu.Unlock()
}

func (h *Hub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// ServeHTTP implements GET /api/stream.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, api.ErrInternal, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sub := h.subscribe()
	defer h.unsubscribe(sub)

	ticker := time.NewTicker(PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-sub.ch:
			writeSSE(w, msg.event, msg.data)
			flusher.Flush()
		case <-ticker.C:
			writeSSE(w, api.SSEPing, struct{}{})
			flusher.Flush()
		}
	}
}

func writeSSE(w io.Writer, event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		b = []byte("{}")
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}

// FromNotice converts a watcher notice into an SSE event name and payload.
func FromNotice(n watcher.Notice) (string, any) {
	return api.SSEDoc, api.SSEDocChange{
		Doc:     n.DocID,
		Kind:    n.Kind,
		Rev:     n.Rev,
		Remaps:  n.Remaps,
		Orphans: n.Orphans,
	}
}
