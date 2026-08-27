package server

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/six-ddc/art/internal/eventlog"
)

// fakeAppender is an in-memory eventAppender that records each Append call,
// used to verify the Writer's serialization behavior without depending on the
// real eventlog.Store implementation (which still panics at the skeleton
// stage).
type fakeAppender struct {
	mu     sync.Mutex
	calls  int
	events []eventlog.Event
}

func (f *fakeAppender) Append(docID string, events ...eventlog.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.events = append(f.events, events...)
	return nil
}

func (f *fakeAppender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

// newTestWriter bypasses NewWriter (whose exported signature is frozen to
// accept a *eventlog.Store), injecting the fake directly so we can unit test
// the Writer's own serialization logic.
func newTestWriter(store eventAppender) *Writer {
	return &Writer{store: store, reqs: make(chan WriteRequest, 256)}
}

// server: Writer serialization -- after 100 concurrent Appends, all events
// should be fully parsable (here we verify no loss or interleaved writes by
// checking the total event count and call count received by the fake store).
func TestWriterSerializesConcurrentAppends(t *testing.T) {
	fake := &fakeAppender{}
	w := newTestWriter(fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			ev := eventlog.Event{E: eventlog.KindReply, Body: fmt.Sprintf("body-%d", i)}
			if err := w.Append(ctx, "doc1", ev); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Append returned an error: %v", err)
	}

	if got := fake.count(); got != goroutines {
		t.Fatalf("fake store received %d events, want %d", got, goroutines)
	}
	if fake.calls != goroutines {
		t.Fatalf("each Append should correspond to one serialized store.Append call, got %d calls", fake.calls)
	}

	// Integrity check: every body-i appears, and appears exactly once.
	seen := make(map[string]bool)
	for _, ev := range fake.events {
		if seen[ev.Body] {
			t.Fatalf("duplicate event: %s", ev.Body)
		}
		seen[ev.Body] = true
	}
	if len(seen) != goroutines {
		t.Fatalf("unique event count = %d, want %d", len(seen), goroutines)
	}
}

func TestWriterPropagatesStoreError(t *testing.T) {
	fake := &erroringAppender{}
	w := newTestWriter(fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	err := w.Append(ctx, "doc1", eventlog.Event{E: eventlog.KindReply})
	if err == nil {
		t.Fatal("Append should propagate the error when the store returns one")
	}
}

type erroringAppender struct{}

func (erroringAppender) Append(docID string, events ...eventlog.Event) error {
	return fmt.Errorf("boom")
}
