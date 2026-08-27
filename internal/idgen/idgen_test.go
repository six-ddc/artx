package idgen

import (
	"strings"
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

func TestEventIDUnique(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 1000; i++ {
		id := EventID()
		if id == "" {
			t.Fatal("EventID() returned empty string")
		}
		if !strings.Contains(id, "-") {
			t.Fatalf("EventID() = %q, want a '-' separator", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != 1000 {
		t.Fatalf("got %d unique event ids out of 1000", len(seen))
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
