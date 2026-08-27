package eventlog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/six-ddc/artx/internal/anchor"
	"github.com/six-ddc/artx/internal/api"
)

func createEvent(eid, ts, thread, author, body string) Event {
	return Event{E: KindCreate, EID: eid, Thread: thread, Author: author, Body: body,
		Anchor: &anchor.Anchor{Kind: api.AnchorText, Exact: body, Start: 0, End: len(body)},
		TS:     mustTimeQuiet(ts)}
}

func mustTimeQuiet(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return tm
}

// ---------------------------------------------------------------------------
// Fold semantics — one case per §4.4 rule.
// ---------------------------------------------------------------------------

func TestFoldDedupByEID(t *testing.T) {
	// Rule 1: dedup by eid — merge=union can duplicate a block verbatim.
	e := createEvent("e1", "2026-08-24T10:00:00Z", "cabcde", "cappu", "hello")
	events := []Event{e, e} // exact duplicate, as union merge would produce
	fr := Fold(events)
	if len(fr.Threads) != 1 {
		t.Fatalf("Threads = %d, want 1 (deduped)", len(fr.Threads))
	}
	if len(fr.Threads[0].Replies) != 0 {
		t.Fatalf("unexpected replies: %+v", fr.Threads[0].Replies)
	}
}

func TestFoldStableSortByTSThenEID(t *testing.T) {
	// Rule 2: sort by ts asc, tie-break eid asc; physical order must not matter.
	// Two replies with out-of-order physical placement but well-formed ts/eid
	// must come out in ts order regardless of the slice order fed in.
	create := createEvent("e0", "2026-08-24T10:00:00Z", "cabcde", "cappu", "root")
	r2 := Event{E: KindReply, EID: "e2", Thread: "cabcde", ID: "cabcde.002", Author: "a", Body: "second", TS: mustTimeQuiet("2026-08-24T10:02:00Z")}
	r1 := Event{E: KindReply, EID: "e1", Thread: "cabcde", ID: "cabcde.001", Author: "a", Body: "first", TS: mustTimeQuiet("2026-08-24T10:01:00Z")}

	// Fed in physically "wrong" order: r2 before r1.
	fr := Fold([]Event{create, r2, r1})
	if len(fr.Threads) != 1 || len(fr.Threads[0].Replies) != 2 {
		t.Fatalf("unexpected fold result: %+v", fr.Threads)
	}
	if fr.Threads[0].Replies[0].Body != "first" || fr.Threads[0].Replies[1].Body != "second" {
		t.Fatalf("replies not in ts order: %+v", fr.Threads[0].Replies)
	}
}

func TestFoldSortTieBreakByEID(t *testing.T) {
	// Same ts, different eid: eid ascending breaks the tie deterministically.
	create := createEvent("e0", "2026-08-24T10:00:00Z", "cabcde", "cappu", "root")
	ts := "2026-08-24T10:05:00Z"
	rB := Event{E: KindReply, EID: "b", Thread: "cabcde", ID: "cabcde.00b", Body: "B", TS: mustTimeQuiet(ts)}
	rA := Event{E: KindReply, EID: "a", Thread: "cabcde", ID: "cabcde.00a", Body: "A", TS: mustTimeQuiet(ts)}

	fr := Fold([]Event{create, rB, rA})
	if fr.Threads[0].Replies[0].Body != "A" || fr.Threads[0].Replies[1].Body != "B" {
		t.Fatalf("eid tie-break failed: %+v", fr.Threads[0].Replies)
	}
}

func TestFoldNoTSSortsFirst(t *testing.T) {
	// "events with no ts sort to the front": a zero-value ts must sort before any real ts.
	withTS := createEvent("e2", "2026-08-24T10:00:00Z", "cabcde", "cappu", "later")
	noTS := Event{E: KindCreate, EID: "e1", Thread: "cabcd0", Author: "cappu", Body: "earlier",
		Anchor: &anchor.Anchor{Kind: api.AnchorText, Exact: "earlier"}}

	fr := Fold([]Event{withTS, noTS})
	if len(fr.Threads) != 2 {
		t.Fatalf("Threads = %d, want 2", len(fr.Threads))
	}
	// Output is sorted by CreatedAt ascending, so the no-ts thread (zero
	// time, earliest) must come first.
	if fr.Threads[0].Thread != "cabcd0" {
		t.Fatalf("threads[0] = %s, want cabcd0 (no-ts create sorts first)", fr.Threads[0].Thread)
	}
}

func TestFoldDuplicateCreateGoesToWarnings(t *testing.T) {
	// Rule 3: same thread, two creates -> first (by sort order) wins, second warns.
	first := createEvent("e1", "2026-08-24T10:00:00Z", "cabcde", "cappu", "first body")
	second := createEvent("e2", "2026-08-24T10:05:00Z", "cabcde", "other", "second body")

	fr := Fold([]Event{first, second})
	if len(fr.Threads) != 1 {
		t.Fatalf("Threads = %d, want 1", len(fr.Threads))
	}
	if fr.Threads[0].Body != "first body" {
		t.Fatalf("Body = %q, want the first create's body", fr.Threads[0].Body)
	}
	if len(fr.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want exactly one warning about the duplicate create", fr.Warnings)
	}
}

func TestFoldReplyDedupByID(t *testing.T) {
	// Rule 4: reply dedup by id.
	create := createEvent("e0", "2026-08-24T10:00:00Z", "cabcde", "cappu", "root")
	r1 := Event{E: KindReply, EID: "e1", Thread: "cabcde", ID: "cabcde.001", Body: "hi", TS: mustTimeQuiet("2026-08-24T10:01:00Z")}
	r1dup := Event{E: KindReply, EID: "e2", Thread: "cabcde", ID: "cabcde.001", Body: "hi again (dup id, different eid)", TS: mustTimeQuiet("2026-08-24T10:02:00Z")}

	fr := Fold([]Event{create, r1, r1dup})
	if len(fr.Threads[0].Replies) != 1 {
		t.Fatalf("Replies = %+v, want exactly 1 (deduped by id)", fr.Threads[0].Replies)
	}
	if fr.Threads[0].Replies[0].Body != "hi" {
		t.Fatalf("Body = %q, want the first reply's body kept", fr.Threads[0].Replies[0].Body)
	}
}

func TestFoldEditOverridesRootAndReply(t *testing.T) {
	// Rule 5: edit overrides target's body and records EditedAt; target ==
	// thread id edits the root comment, target == reply id edits that reply.
	create := createEvent("e0", "2026-08-24T10:00:00Z", "cabcde", "cappu", "original root")
	reply := Event{E: KindReply, EID: "e1", Thread: "cabcde", ID: "cabcde.001", Body: "original reply", TS: mustTimeQuiet("2026-08-24T10:01:00Z")}
	editRoot := Event{E: KindEdit, EID: "e2", Target: "cabcde", Body: "edited root", TS: mustTimeQuiet("2026-08-24T10:02:00Z")}
	editReply := Event{E: KindEdit, EID: "e3", Target: "cabcde.001", Body: "edited reply", TS: mustTimeQuiet("2026-08-24T10:03:00Z")}

	fr := Fold([]Event{create, reply, editRoot, editReply})
	th := fr.Threads[0]
	if th.Body != "edited root" {
		t.Fatalf("root Body = %q, want %q", th.Body, "edited root")
	}
	if th.EditedAt == nil || !th.EditedAt.Equal(mustTimeQuiet("2026-08-24T10:02:00Z")) {
		t.Fatalf("root EditedAt = %v, want 10:02:00Z", th.EditedAt)
	}
	if th.Replies[0].Body != "edited reply" {
		t.Fatalf("reply Body = %q, want %q", th.Replies[0].Body, "edited reply")
	}
	if th.Replies[0].EditedAt == nil || !th.Replies[0].EditedAt.Equal(mustTimeQuiet("2026-08-24T10:03:00Z")) {
		t.Fatalf("reply EditedAt = %v, want 10:03:00Z", th.Replies[0].EditedAt)
	}
}

func TestFoldStatusTakesLastByOrder(t *testing.T) {
	// Rule 6: status = the last status-kind event in sorted order, via StatusKinds.
	create := createEvent("e0", "2026-08-24T10:00:00Z", "cabcde", "cappu", "root")
	addressed := Event{E: KindAddressed, EID: "e1", Thread: "cabcde", By: "agent:claude", TS: mustTimeQuiet("2026-08-24T10:01:00Z")}
	resolved := Event{E: KindResolve, EID: "e2", Thread: "cabcde", By: "cappu", TS: mustTimeQuiet("2026-08-24T10:02:00Z")}
	reopened := Event{E: KindReopen, EID: "e3", Thread: "cabcde", By: "cappu", Note: "not quite", TS: mustTimeQuiet("2026-08-24T10:03:00Z")}

	// Feed out of order to prove sort order (not append order) determines status.
	fr := Fold([]Event{create, reopened, resolved, addressed})
	if fr.Threads[0].Status != api.StatusOpen {
		t.Fatalf("Status = %q, want %q (last event by ts is reopen)", fr.Threads[0].Status, api.StatusOpen)
	}

	fr2 := Fold([]Event{create, addressed, resolved})
	if fr2.Threads[0].Status != api.StatusResolved {
		t.Fatalf("Status = %q, want %q", fr2.Threads[0].Status, api.StatusResolved)
	}
	if fr2.Threads[0].Addressed == nil || fr2.Threads[0].Addressed.By != "agent:claude" {
		t.Fatalf("Addressed = %+v, want recorded from the addressed event", fr2.Threads[0].Addressed)
	}
}

func TestFoldRemapOverwritesAndClearsOrphan(t *testing.T) {
	// Rule 7: remap takes the last one, overwrites Start/End/Rev, clears orphan.
	create := createEvent("e0", "2026-08-24T10:00:00Z", "cabcde", "cappu", "root")
	orphan := Event{E: KindOrphan, EID: "e1", Thread: "cabcde", LastExact: "root", TS: mustTimeQuiet("2026-08-24T10:01:00Z")}
	remap1 := Event{E: KindRemap, EID: "e2", Thread: "cabcde", Start: 10, End: 20, Rev: "aaa111", TS: mustTimeQuiet("2026-08-24T10:02:00Z")}
	remap2 := Event{E: KindRemap, EID: "e3", Thread: "cabcde", Start: 30, End: 40, Rev: "bbb222", TS: mustTimeQuiet("2026-08-24T10:03:00Z")}

	fr := Fold([]Event{create, orphan, remap1, remap2})
	th := fr.Threads[0]
	if th.Anchor.Start != 30 || th.Anchor.End != 40 || th.Anchor.Rev != "bbb222" {
		t.Fatalf("Anchor = %+v, want last remap's start/end/rev", th.Anchor)
	}
	if th.Anchor.Orphan || th.Hint != "" {
		t.Fatalf("thread still orphaned after remap: Anchor=%+v Hint=%q", th.Anchor, th.Hint)
	}
}

func TestFoldOrphanSetsHintAndLastExact(t *testing.T) {
	// Rule 8: orphan sets Orphan + LastExact + the fixed Hint string, and a
	// later remap clears it again (revival).
	create := createEvent("e0", "2026-08-24T10:00:00Z", "cabcde", "cappu", "root")
	orphan := Event{E: KindOrphan, EID: "e1", Thread: "cabcde", LastExact: "root", TS: mustTimeQuiet("2026-08-24T10:01:00Z")}

	fr := Fold([]Event{create, orphan})
	th := fr.Threads[0]
	if !th.Anchor.Orphan || th.Anchor.LastExact != "root" || th.Hint != api.OrphanHint {
		t.Fatalf("orphan state = %+v hint=%q, want Orphan=true LastExact=root Hint=%q", th.Anchor, th.Hint, api.OrphanHint)
	}

	revive := Event{E: KindRemap, EID: "e2", Thread: "cabcde", Start: 5, End: 9, TS: mustTimeQuiet("2026-08-24T10:02:00Z")}
	fr2 := Fold([]Event{create, orphan, revive})
	th2 := fr2.Threads[0]
	if th2.Anchor.Orphan || th2.Hint != "" {
		t.Fatalf("thread not revived: %+v hint=%q", th2.Anchor, th2.Hint)
	}
}

func TestFoldUnknownEventAndOrphanReferenceGoToWarnings(t *testing.T) {
	// Rule 9: events referencing a nonexistent thread, and unknown `e`
	// values, are dropped and reported as warnings — not errors.
	create := createEvent("e0", "2026-08-24T10:00:00Z", "cabcde", "cappu", "root")
	ghostReply := Event{E: KindReply, EID: "e1", Thread: "cffffff", ID: "cffffff.001", Body: "orphan ref", TS: mustTimeQuiet("2026-08-24T10:01:00Z")}
	unknownKind := Event{E: "teleport", EID: "e2", Thread: "cabcde", TS: mustTimeQuiet("2026-08-24T10:02:00Z")}

	fr := Fold([]Event{create, ghostReply, unknownKind})
	if len(fr.Threads) != 1 {
		t.Fatalf("Threads = %d, want 1 (ghost reply must not create a thread)", len(fr.Threads))
	}
	if len(fr.Warnings) != 2 {
		t.Fatalf("Warnings = %v, want 2 entries", fr.Warnings)
	}
}

func TestFoldUpdatedAtIsMaxEventTS(t *testing.T) {
	// Rule 10: UpdatedAt = max ts across all of the thread's events.
	create := createEvent("e0", "2026-08-24T10:00:00Z", "cabcde", "cappu", "root")
	reply := Event{E: KindReply, EID: "e1", Thread: "cabcde", ID: "cabcde.001", Body: "hi", TS: mustTimeQuiet("2026-08-24T12:00:00Z")}

	fr := Fold([]Event{create, reply})
	want := mustTimeQuiet("2026-08-24T12:00:00Z")
	if !fr.Threads[0].UpdatedAt.Equal(want) {
		t.Fatalf("UpdatedAt = %v, want %v", fr.Threads[0].UpdatedAt, want)
	}
}

func TestFoldOutputSortedByCreatedAt(t *testing.T) {
	// Rule 11: output threads sorted by CreatedAt ascending; Doc/Slug/Path
	// and Anchor.Line/Context are left for the caller to fill in.
	later := createEvent("e2", "2026-08-24T11:00:00Z", "cbbbbb", "x", "second")
	earlier := createEvent("e1", "2026-08-24T10:00:00Z", "caaaaa", "x", "first")

	fr := Fold([]Event{later, earlier})
	if fr.Threads[0].Thread != "caaaaa" || fr.Threads[1].Thread != "cbbbbb" {
		t.Fatalf("threads not sorted by CreatedAt: %+v", fr.Threads)
	}
	for _, th := range fr.Threads {
		if th.Doc != "" || th.Slug != "" || th.Path != "" {
			t.Fatalf("Doc/Slug/Path should be left empty by Fold: %+v", th)
		}
		if th.Anchor.Line != 0 || th.Anchor.Context != "" {
			t.Fatalf("Anchor.Line/Context should be left empty by Fold: %+v", th.Anchor)
		}
	}
}

// TestFoldCombinedChaosCase is the required "out-of-order + duplicate eid +
// unknown event kind + orphan event, combined" acceptance case — the one
// that most directly stands in for correctness after a real git
// merge=union union.
func TestFoldCombinedChaosCase(t *testing.T) {
	create := createEvent("e0", "2026-08-24T10:00:00Z", "cabcde", "cappu", "root")
	createDup := create // exact duplicate block, as union merge produces
	reply1 := Event{E: KindReply, EID: "e1", Thread: "cabcde", ID: "cabcde.001", Body: "r1", TS: mustTimeQuiet("2026-08-24T10:01:00Z")}
	reply1dup := reply1 // duplicate eid AND duplicate reply id
	unknown := Event{E: "bogus", EID: "e2", Thread: "cabcde", TS: mustTimeQuiet("2026-08-24T10:02:00Z")}
	ghost := Event{E: KindReply, EID: "e3", Thread: "cnothing", ID: "cnothing.001", Body: "ghost", TS: mustTimeQuiet("2026-08-24T10:03:00Z")}
	orphan := Event{E: KindOrphan, EID: "e4", Thread: "cabcde", LastExact: "root", TS: mustTimeQuiet("2026-08-24T10:04:00Z")}
	remap := Event{E: KindRemap, EID: "e5", Thread: "cabcde", Start: 1, End: 2, TS: mustTimeQuiet("2026-08-24T10:05:00Z")}

	// Physically shuffled, as a union merge of two divergent branches would produce.
	events := []Event{remap, ghost, reply1dup, unknown, orphan, create, reply1, createDup}

	fr := Fold(events)
	if len(fr.Threads) != 1 {
		t.Fatalf("Threads = %d, want 1", len(fr.Threads))
	}
	th := fr.Threads[0]
	if len(th.Replies) != 1 {
		t.Fatalf("Replies = %+v, want exactly 1 (eid + id dedup)", th.Replies)
	}
	if th.Anchor.Orphan {
		t.Fatalf("thread should be revived by the later remap: %+v", th.Anchor)
	}
	if th.Anchor.Start != 1 || th.Anchor.End != 2 {
		t.Fatalf("Anchor = %+v, want start=1 end=2 from remap", th.Anchor)
	}
	if len(fr.Warnings) != 2 {
		t.Fatalf("Warnings = %v, want exactly 2 (unknown kind + ghost reply)", fr.Warnings)
	}
}

// TestFoldThreadWithNoRepliesMarshalsToEmptyArray is the BLOCKER-1 regression
// test: api.Thread.Replies has no `omitempty` tag (frozen contract), so a
// nil slice there marshals to JSON `null` instead of `[]` and breaks any
// consumer (the frontend's TS types, in particular) that expects a
// non-nullable array. A thread with zero replies must still produce
// "replies":[].
func TestFoldThreadWithNoRepliesMarshalsToEmptyArray(t *testing.T) {
	create := createEvent("e1", "2026-08-24T10:00:00Z", "cabcde", "cappu", "no replies here")
	fr := Fold([]Event{create})
	if len(fr.Threads) != 1 {
		t.Fatalf("Threads = %d, want 1", len(fr.Threads))
	}
	if fr.Threads[0].Replies == nil {
		t.Fatal("Replies is nil, want a non-nil empty slice")
	}

	data, err := json.Marshal(fr.Threads[0])
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !bytes.Contains(data, []byte(`"replies":[]`)) {
		t.Fatalf("marshaled Thread = %s, want it to contain \"replies\":[]", data)
	}
	if bytes.Contains(data, []byte(`"replies":null`)) {
		t.Fatalf("marshaled Thread = %s, must not contain \"replies\":null", data)
	}
}

// ---------------------------------------------------------------------------
// Read: tail corruption tolerance
// ---------------------------------------------------------------------------

func TestReadFromTailCorruptionTolerated(t *testing.T) {
	good, err := Marshal(
		createEvent("e1", "2026-08-24T10:00:00Z", "cabcde", "cappu", "one"),
		createEvent("e2", "2026-08-24T10:01:00Z", "cbbbbb", "cappu", "two"),
	)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	buf.Write(good)
	tailOffset := buf.Len()
	// A block that got cut off mid-write: marker present, body garbage/incomplete.
	buf.WriteString("---\ne: create\nthread: [unterminated")

	events, report, err := ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom returned an error, want nil: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (the two well-formed blocks)", len(events))
	}
	if !report.TailCorrupt {
		t.Fatal("report.TailCorrupt = false, want true")
	}
	if report.TailOffset != int64(tailOffset) {
		t.Fatalf("report.TailOffset = %d, want %d", report.TailOffset, tailOffset)
	}
	if report.Events != 2 {
		t.Fatalf("report.Events = %d, want 2", report.Events)
	}
}

func TestReadFromEmptyTrailingBlockTolerated(t *testing.T) {
	// A writer that crashed right after emitting "---\n" and nothing else.
	good, err := Marshal(createEvent("e1", "2026-08-24T10:00:00Z", "cabcde", "cappu", "one"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	buf.Write(good)
	buf.WriteString("---\n")

	events, report, err := ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if !report.TailCorrupt {
		t.Fatal("report.TailCorrupt = false, want true")
	}
}

func TestStoreReadMissingFile(t *testing.T) {
	s := Open(t.TempDir())
	events, report, err := s.Read("nosuchdoc")
	if err != nil {
		t.Fatalf("Read on missing file returned error: %v", err)
	}
	if len(events) != 0 || report.Events != 0 {
		t.Fatalf("Read on missing file = %v %+v, want empty", events, report)
	}
}

// ---------------------------------------------------------------------------
// Append: concurrency safety
// ---------------------------------------------------------------------------

func TestAppendConcurrentSafety(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	const goroutines = 8
	const perGoroutine = 50

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				ev := createEvent("", "", fmt.Sprintf("c%d%04d", g, i), "cappu", fmt.Sprintf("g%d-i%d", g, i))
				ev.EID = "" // force Append to assign a fresh eid
				ev.TS = time.Time{}
				if err := s.Append("docid1", ev); err != nil {
					t.Errorf("Append: %v", err)
				}
			}
		}(g)
	}
	wg.Wait()

	events, report, err := s.Read("docid1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if report.TailCorrupt {
		t.Fatalf("report.TailCorrupt = true after concurrent append, warnings=%v", report.Warnings)
	}
	if len(events) != goroutines*perGoroutine {
		t.Fatalf("events = %d, want %d", len(events), goroutines*perGoroutine)
	}

	seenEID := make(map[string]bool, len(events))
	for _, e := range events {
		if e.EID == "" {
			t.Fatal("event with empty eid found after Append")
		}
		if seenEID[e.EID] {
			t.Fatalf("duplicate eid after concurrent append: %s", e.EID)
		}
		seenEID[e.EID] = true
	}

	fr := Fold(events)
	if len(fr.Threads) != goroutines*perGoroutine {
		t.Fatalf("Fold produced %d threads, want %d", len(fr.Threads), goroutines*perGoroutine)
	}
}

// ---------------------------------------------------------------------------
// Compact
// ---------------------------------------------------------------------------

func threadSnapshot(threads []api.Thread) map[string]api.Thread {
	m := make(map[string]api.Thread, len(threads))
	for _, th := range threads {
		th.Anchor.Line, th.Anchor.Context = 0, "" // caller-filled fields, irrelevant here
		m[th.Thread] = th
	}
	return m
}

func TestCompactPreservesFoldEquivalenceForKeptThreads(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	docID := "docabc"

	// Thread 1: open, with an edit chain and a remap chain — should survive
	// compaction with equivalent folded state (collapsed onto fewer events).
	c1 := createEvent("e1", "2026-08-24T09:00:00Z", "caaaaa", "cappu", "original")
	edit1 := Event{E: KindEdit, EID: "e2", Target: "caaaaa", Body: "edited body", TS: mustTimeQuiet("2026-08-24T09:01:00Z")}
	remap1 := Event{E: KindRemap, EID: "e3", Thread: "caaaaa", Start: 5, End: 15, Rev: "cafe123", TS: mustTimeQuiet("2026-08-24T09:02:00Z")}
	reply1 := Event{E: KindReply, EID: "e4", Thread: "caaaaa", ID: "caaaaa.001", Author: "cappu", Body: "a reply", TS: mustTimeQuiet("2026-08-24T09:03:00Z")}

	// Thread 2: resolved a long time ago — should be archived away.
	c2 := createEvent("e5", "2026-06-01T09:00:00Z", "cbbbbb", "cappu", "old thread")
	resolve2 := Event{E: KindResolve, EID: "e6", Thread: "cbbbbb", By: "cappu", TS: mustTimeQuiet("2026-06-01T09:05:00Z")}

	if err := s.Append(docID, c1, edit1, remap1, reply1, c2, resolve2); err != nil {
		t.Fatalf("Append: %v", err)
	}

	beforeEvents, _, err := s.Read(docID)
	if err != nil {
		t.Fatal(err)
	}
	beforeFold := Fold(beforeEvents)
	beforeSnapshot := threadSnapshot(beforeFold.Threads)

	now := mustTimeQuiet("2026-08-25T00:00:00Z") // > 30 days after thread 2's resolve
	stat, err := s.Compact(docID, CompactOptions{Now: now, Force: true})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if stat.ThreadsArchived != 1 {
		t.Fatalf("ThreadsArchived = %d, want 1", stat.ThreadsArchived)
	}
	if stat.EventsAfter >= stat.EventsBefore {
		t.Fatalf("EventsAfter(%d) should be < EventsBefore(%d)", stat.EventsAfter, stat.EventsBefore)
	}

	afterEvents, _, err := s.Read(docID)
	if err != nil {
		t.Fatal(err)
	}
	afterFold := Fold(afterEvents)
	afterSnapshot := threadSnapshot(afterFold.Threads)

	// Thread 2 (archived) must be gone from the active file's fold.
	if _, ok := afterSnapshot["cbbbbb"]; ok {
		t.Fatal("archived thread cbbbbb still present in post-compact fold")
	}

	// Thread 1 (kept) must fold to an equivalent state before and after,
	// aside from CreatedRev/EditedAt bookkeeping that compaction is allowed
	// to simplify by baking the edit directly into the create event.
	before1, after1 := beforeSnapshot["caaaaa"], afterSnapshot["caaaaa"]
	if before1.Body != after1.Body {
		t.Fatalf("Body mismatch: before=%q after=%q", before1.Body, after1.Body)
	}
	if before1.Status != after1.Status {
		t.Fatalf("Status mismatch: before=%q after=%q", before1.Status, after1.Status)
	}
	if before1.Anchor.Start != after1.Anchor.Start || before1.Anchor.End != after1.Anchor.End || before1.Anchor.Rev != after1.Anchor.Rev {
		t.Fatalf("Anchor mismatch: before=%+v after=%+v", before1.Anchor, after1.Anchor)
	}
	if len(before1.Replies) != len(after1.Replies) || before1.Replies[0].Body != after1.Replies[0].Body {
		t.Fatalf("Replies mismatch: before=%+v after=%+v", before1.Replies, after1.Replies)
	}

	// The archive file must carry the folded snapshot of the archived thread.
	archiveEvents, _, err := ReadFrom(mustOpen(t, s.ArchivePath(docID)))
	if err != nil {
		t.Fatal(err)
	}
	if len(archiveEvents) != 1 || archiveEvents[0].E != KindArchive {
		t.Fatalf("archive file events = %+v, want one archive event", archiveEvents)
	}
	if archiveEvents[0].Archived == nil || archiveEvents[0].Archived.Thread != "cbbbbb" {
		t.Fatalf("archived snapshot = %+v, want thread cbbbbb", archiveEvents[0].Archived)
	}
	if archiveEvents[0].Archived.Body != "old thread" {
		t.Fatalf("archived body = %q, want %q", archiveEvents[0].Archived.Body, "old thread")
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestCompactSkipsWhenBelowThresholdAndNotForced(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	docID := "docsmall"
	if err := s.Append(docID, createEvent("e1", "2026-08-24T10:00:00Z", "caaaaa", "cappu", "hi")); err != nil {
		t.Fatal(err)
	}

	stat, err := s.Compact(docID, CompactOptions{})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !stat.Skipped {
		t.Fatal("Skipped = false, want true (below threshold, no force)")
	}
	if stat.EventsAfter != stat.EventsBefore {
		t.Fatalf("Compact modified event count while skipped: before=%d after=%d", stat.EventsBefore, stat.EventsAfter)
	}
}

func TestNeedsCompactBySizeAndAge(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	docID := "docx"
	if err := s.Append(docID, createEvent("e1", "2026-08-24T10:00:00Z", "caaaaa", "cappu", "hi")); err != nil {
		t.Fatal(err)
	}

	needs, err := s.NeedsCompact(docID, CompactOptions{SizeBytes: 1}) // tiny threshold, always exceeded
	if err != nil {
		t.Fatal(err)
	}
	if !needs {
		t.Fatal("NeedsCompact(tiny size threshold) = false, want true")
	}

	needs, err = s.NeedsCompact(docID, CompactOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if needs {
		t.Fatal("NeedsCompact(default thresholds, tiny fresh doc) = true, want false")
	}
}

// ---------------------------------------------------------------------------
// Truncate
// ---------------------------------------------------------------------------

func TestTruncateDropsTrailingEvents(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	docID := "doctrunc"
	if err := s.Append(docID,
		createEvent("e1", "2026-08-24T10:00:00Z", "caaaaa", "cappu", "one"),
		createEvent("e2", "2026-08-24T10:01:00Z", "cbbbbb", "cappu", "two"),
		createEvent("e3", "2026-08-24T10:02:00Z", "cccccc", "cappu", "three"),
	); err != nil {
		t.Fatal(err)
	}

	if err := s.Truncate(docID, 2); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	events, report, err := s.Read(docID)
	if err != nil {
		t.Fatal(err)
	}
	if report.TailCorrupt {
		t.Fatal("report.TailCorrupt = true after Truncate, want a clean file")
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
}

// ---------------------------------------------------------------------------
// Path / DocIDs
// ---------------------------------------------------------------------------

func TestStorePaths(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if s.Root() != root {
		t.Errorf("Root() = %q, want %q", s.Root(), root)
	}
	want := filepath.Join(root, CommentsDir, "abc123.yaml")
	if got := s.Path("abc123"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
	wantArchive := filepath.Join(root, CommentsDir, "abc123.archive.yaml")
	if got := s.ArchivePath("abc123"); got != wantArchive {
		t.Errorf("ArchivePath() = %q, want %q", got, wantArchive)
	}
}

func TestDocIDsListsActiveOnly(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if err := s.Append("docb", createEvent("e1", "2026-08-24T10:00:00Z", "caaaaa", "cappu", "x")); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("doca", createEvent("e2", "2026-08-24T10:00:00Z", "cbbbbb", "cappu", "x")); err != nil {
		t.Fatal(err)
	}
	// An archive file must not show up as a doc id.
	if err := os.WriteFile(s.ArchivePath("doca"), []byte("---\ne: archive\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ids, err := s.DocIDs()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "doca,docb" {
		t.Fatalf("DocIDs() = %v, want sorted [doca docb]", ids)
	}
}

func TestDocIDsNoCommentsDir(t *testing.T) {
	s := Open(t.TempDir())
	ids, err := s.DocIDs()
	if err != nil {
		t.Fatalf("DocIDs on fresh vault: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("DocIDs = %v, want empty", ids)
	}
}

func TestNewEventFillsEIDAndTS(t *testing.T) {
	e := NewEvent(KindCreate)
	if e.E != KindCreate {
		t.Errorf("E = %q, want %q", e.E, KindCreate)
	}
	if e.EID == "" {
		t.Error("EID not filled")
	}
	if e.TS.IsZero() {
		t.Error("TS not filled")
	}
}
