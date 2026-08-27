package server

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/six-ddc/artx/internal/eventlog"
)

// eventlog now has a real implementation (no longer a panic skeleton); add an
// end-to-end concurrent Append test: the Writer writes to disk through a real
// eventlog.Store, and all 100 events in the event file should be fully
// parsable. This is a real integration check on top of
// TestWriterSerializesConcurrentAppends (which uses a fake store).
func TestWriterConcurrentAppendsAreFullyParsableOnDisk(t *testing.T) {
	root := t.TempDir()
	store := eventlog.Open(root)
	w := NewWriter(store)

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
			ev := eventlog.Event{E: eventlog.KindReply, Thread: "cabcde", Body: fmt.Sprintf("body-%d", i)}
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

	events, report, err := store.Read("doc1")
	if err != nil {
		t.Fatalf("Read returned an error: %v", err)
	}
	if report.TailCorrupt {
		t.Fatalf("event stream should not have a corrupt tail: %+v", report)
	}
	if len(events) != goroutines {
		t.Fatalf("event count on disk = %d, want %d", len(events), goroutines)
	}

	seen := make(map[string]bool)
	for _, ev := range events {
		if ev.Body == "" {
			t.Fatalf("event was parsed incompletely: %+v", ev)
		}
		if seen[ev.Body] {
			t.Fatalf("duplicate event: %s", ev.Body)
		}
		seen[ev.Body] = true
	}
}
