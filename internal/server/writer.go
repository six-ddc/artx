package server

import (
	"context"

	"github.com/six-ddc/art/internal/eventlog"
)

// eventAppender is the minimal surface of eventlog.Store that Writer
// depends on.
//
// The sole reason it exists: NewWriter's exported signature is frozen to
// accept *eventlog.Store, but that package could still be a panicking
// skeleton before integration. Declaring the internal store field as an
// interface lets tests inject an in-memory fake to verify Writer's own
// serialization logic without depending on the real eventlog implementation.
type eventAppender interface {
	Append(docID string, events ...eventlog.Event) error
}

// WriteRequest is a single write submitted to the writer goroutine.
type WriteRequest struct {
	DocID  string
	Events []eventlog.Event
	Reply  chan error
}

// Writer serializes all event-log writes.
type Writer struct {
	store eventAppender
	reqs  chan WriteRequest
}

// NewWriter creates the single writer.
func NewWriter(store *eventlog.Store) *Writer {
	return &Writer{store: store, reqs: make(chan WriteRequest, 256)}
}

// Run blocks consuming write requests until ctx is canceled.
func (w *Writer) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-w.reqs:
			err := w.store.Append(req.DocID, req.Events...)
			if req.Reply != nil {
				req.Reply <- err
			}
		}
	}
}

// Append synchronously submits a write and waits for the result. Every
// handler and the watcher must go through it — none may call
// eventlog.Store.Append directly.
func (w *Writer) Append(ctx context.Context, docID string, events ...eventlog.Event) error {
	reply := make(chan error, 1)
	req := WriteRequest{DocID: docID, Events: events, Reply: reply}
	select {
	case w.reqs <- req:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
