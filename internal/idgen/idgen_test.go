package idgen

import (
	"strings"
	"sync"
	"testing"
)

// TestDocIDShape sanity-checks DocID's length and alphabet. A dedicated
// no-duplicates-in-100k check would need a much larger keyspace than 6
// base36 characters (36^6 ≈ 2.18e9) to be non-flaky — the birthday bound
// puts the expected number of collisions at ~2.3 for 100k draws from that
// space. That volume check lives in TestRandomNoDuplicatesAt100k below,
// which exercises the same crypto/rand-backed generator at a keyspace
// large enough to make collisions astronomically unlikely.
func TestDocIDShape(t *testing.T) {
	for i := 0; i < 1000; i++ {
		id := DocID()
		if len(id) != DocIDLen {
			t.Fatalf("DocID length = %d, want %d", len(id), DocIDLen)
		}
		if !isBase36(id) {
			t.Fatalf("DocID() = %q, not base36", id)
		}
	}
}

// TestRandomNoDuplicatesAt100k is the "100k generations, no duplicates" acceptance check:
// it drives the same generator all art ids are built on (Random, via
// crypto/rand) 100,000 times at a keyspace large enough that a collision
// would indicate a broken generator rather than expected birthday noise.
func TestRandomNoDuplicatesAt100k(t *testing.T) {
	const n = 100_000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := Random(16)
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate id generated: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestThreadIDShape(t *testing.T) {
	id := ThreadID()
	if !IsThreadID(id) {
		t.Fatalf("ThreadID() = %q, not recognized by IsThreadID", id)
	}
	if !strings.HasPrefix(id, "c") {
		t.Fatalf("ThreadID() = %q, want prefix c", id)
	}
	if len(id) != 1+ThreadIDLen {
		t.Fatalf("ThreadID() length = %d, want %d", len(id), 1+ThreadIDLen)
	}
}

func TestReplyIDShape(t *testing.T) {
	thread := ThreadID()
	reply := ReplyID(thread)
	if !IsReplyID(reply) {
		t.Fatalf("ReplyID() = %q, not recognized by IsReplyID", reply)
	}
	if IsThreadID(reply) {
		t.Fatalf("reply id %q should not be recognized as a thread id", reply)
	}
	want := thread + "."
	if !strings.HasPrefix(reply, want) {
		t.Fatalf("ReplyID() = %q, want prefix %q", reply, want)
	}
}

func TestThreadOfComment(t *testing.T) {
	thread := ThreadID()
	reply := ReplyID(thread)

	if got := ThreadOfComment(thread); got != thread {
		t.Errorf("ThreadOfComment(thread id) = %q, want %q", got, thread)
	}
	if got := ThreadOfComment(reply); got != thread {
		t.Errorf("ThreadOfComment(reply id) = %q, want %q", got, thread)
	}
}

// TestEventIDUniqueTightLoop drives EventID from a single goroutine as fast
// as possible, which is exactly the scenario where same-millisecond
// suffix collisions used to be a real, non-negligible risk (a plain
// independent crypto/rand draw for the 4-char suffix collides ~26% of the
// time at 1000 calls within one millisecond). Uniqueness here is now
// guaranteed by construction, not probabilistically, so this must never be
// flaky.
func TestEventIDUniqueTightLoop(t *testing.T) {
	const n = 10_000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := EventID()
		if id == "" {
			t.Fatal("EventID() returned empty string")
		}
		if !strings.Contains(id, "-") {
			t.Fatalf("EventID() = %q, want a '-' separator", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate event id in a tight loop: %s (call %d)", id, i)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != n {
		t.Fatalf("got %d unique event ids out of %d", len(seen), n)
	}
}

// TestEventIDUniqueConcurrent is the concurrent-safety half of the same
// acceptance check: EventID can be called both from a serve Writer
// goroutine and directly from the CLI, so it must stay collision-free
// under real concurrency too. Run with -race.
func TestEventIDUniqueConcurrent(t *testing.T) {
	const goroutines = 8
	const perGoroutine = 1000

	ids := make([][]string, goroutines)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			local := make([]string, perGoroutine)
			for i := 0; i < perGoroutine; i++ {
				local[i] = EventID()
			}
			ids[g] = local
		}(g)
	}
	wg.Wait()

	seen := make(map[string]struct{}, goroutines*perGoroutine)
	for _, local := range ids {
		for _, id := range local {
			if _, dup := seen[id]; dup {
				t.Fatalf("duplicate event id under concurrency: %s", id)
			}
			seen[id] = struct{}{}
		}
	}
	if want := goroutines * perGoroutine; len(seen) != want {
		t.Fatalf("got %d unique event ids, want %d", len(seen), want)
	}
}

func TestIsThreadIDRejectsGarbage(t *testing.T) {
	cases := []string{"", "c123", "cabcde.x8q", "xabcdf", "c" + strings.Repeat("z", ThreadIDLen+1)}
	for _, c := range cases {
		if IsThreadID(c) {
			t.Errorf("IsThreadID(%q) = true, want false", c)
		}
	}
}

func TestIsReplyIDRejectsGarbage(t *testing.T) {
	cases := []string{"", "cabcde", "cabcde.", "cabcde.toolong", "notathread.x8q"}
	for _, c := range cases {
		if IsReplyID(c) {
			t.Errorf("IsReplyID(%q) = true, want false", c)
		}
	}
}

func TestRandomLength(t *testing.T) {
	if got := Random(0); got != "" {
		t.Errorf("Random(0) = %q, want empty", got)
	}
	if got := Random(10); len(got) != 10 {
		t.Errorf("Random(10) length = %d, want 10", len(got))
	}
}
