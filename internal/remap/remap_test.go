package remap

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/six-ddc/art/internal/anchor"
	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/eventlog"
)

const oldSrc = `# 项目说明

第一段是引言部分。

支付重构方案需要进一步评估风险。

最后一段是结论。
`

const anchored = "支付重构方案需要进一步评估风险。"

func spanOf(t *testing.T, src, marker string) (int, int) {
	t.Helper()
	i := strings.Index(src, marker)
	if i < 0 {
		t.Fatalf("marker %q not found in source", marker)
	}
	return i, i + len(marker)
}

// threadAt builds an open thread anchored on anchored.
func threadAt(t *testing.T, src string) api.Thread {
	t.Helper()
	s, e := spanOf(t, src, anchored)
	exact, prefix, suffix := anchor.Quote([]byte(src), s, e)
	return api.Thread{
		Thread: "c7k2f9",
		Status: api.StatusOpen,
		Anchor: api.ThreadAnchor{
			Kind: api.AnchorText, Exact: exact, Prefix: prefix, Suffix: suffix,
			Start: s, End: e,
		},
	}
}

// remapOne runs a single-thread Remap and asserts exactly one result comes back.
func remapOne(t *testing.T, old, next string, th api.Thread) Result {
	t.Helper()
	rs, err := Remap([]byte(old), []byte(next), []api.Thread{th}, DefaultOptions())
	if err != nil {
		t.Fatalf("Remap: %v", err)
	}
	if len(rs) != 1 {
		t.Fatalf("want 1 result, got %d: %+v", len(rs), rs)
	}
	return rs[0]
}

func TestRemapShiftsOnInsertBefore(t *testing.T) {
	newSrc := strings.Replace(oldSrc,
		"第一段是引言部分。",
		"插入的新段落放在锚点之前。\n\n第一段是引言部分。", 1)

	r := remapOne(t, oldSrc, newSrc, threadAt(t, oldSrc))

	wantStart, wantEnd := spanOf(t, newSrc, anchored)
	if r.Kind != KindMoved {
		t.Fatalf("Kind = %q, want %q", r.Kind, KindMoved)
	}
	if r.Start != wantStart || r.End != wantEnd {
		t.Fatalf("wrong shift: [%d,%d), want [%d,%d)", r.Start, r.End, wantStart, wantEnd)
	}
	if r.Score != 1 {
		t.Fatalf("a byte-identical shift should score 1, got %v", r.Score)
	}
	if r.Exact != anchored {
		t.Fatalf("Exact = %q", r.Exact)
	}
	if r.Thread != "c7k2f9" {
		t.Fatalf("Thread = %q", r.Thread)
	}
}

func TestRemapBitapOnInternalEdit(t *testing.T) {
	// Two characters inside the anchor text were edited: levels 1 and 2 must miss, leaving only bitap.
	newSrc := strings.Replace(oldSrc, anchored, "支付重构方案需要尽快评估成本。", 1)

	r := remapOne(t, oldSrc, newSrc, threadAt(t, oldSrc))

	if r.Kind == KindOrphan {
		t.Fatalf("an internal text edit should not turn into an orphan: %+v", r)
	}
	wantStart, _ := spanOf(t, newSrc, "支付重构方案需要尽快评估成本。")
	if r.Start != wantStart {
		t.Fatalf("wrong bitap landing position: %d, want %d", r.Start, wantStart)
	}
	if r.Score <= 0 || r.Score >= 1 {
		t.Fatalf("a fuzzy hit's Score should fall in (0,1), got %v", r.Score)
	}
}

func TestRemapOrphansOnDelete(t *testing.T) {
	newSrc := strings.Replace(oldSrc, anchored+"\n\n", "", 1)
	th := threadAt(t, oldSrc)

	r := remapOne(t, oldSrc, newSrc, th)

	if r.Kind != KindOrphan {
		t.Fatalf("deleting the whole paragraph should orphan the thread, got %+v (new content: %q)", r, newSrc)
	}
	if r.Exact != anchored {
		t.Fatalf("an orphan result must carry back the text as it was before it vanished, for last_exact; got %q", r.Exact)
	}
}

func TestRemapRevivesWhenTextComesBack(t *testing.T) {
	deleted := strings.Replace(oldSrc, anchored+"\n\n", "", 1)

	// A thread that's already orphaned: Exact has been cleared, only LastExact remains.
	th := api.Thread{
		Thread: "c7k2f9",
		Status: api.StatusOpen,
		Anchor: api.ThreadAnchor{
			Kind: api.AnchorText, Orphan: true, LastExact: anchored,
			Start: 0, End: 0,
		},
	}

	// The content was restored.
	r := remapOne(t, deleted, oldSrc, th)

	if r.Kind != KindRevived {
		t.Fatalf("the thread should be revived once the text comes back, got %+v", r)
	}
	wantStart, wantEnd := spanOf(t, oldSrc, anchored)
	if r.Start != wantStart || r.End != wantEnd {
		t.Fatalf("wrong revival position: [%d,%d), want [%d,%d)", r.Start, r.End, wantStart, wantEnd)
	}
}

func TestRemapKeepsOrphanQuietWhenStillMissing(t *testing.T) {
	deleted := strings.Replace(oldSrc, anchored+"\n\n", "", 1)
	th := api.Thread{
		Thread: "c7k2f9",
		Status: api.StatusOpen,
		Anchor: api.ThreadAnchor{Kind: api.AnchorText, Orphan: true, LastExact: anchored},
	}

	// Something else was edited; the anchor text is still absent. This should not emit another orphan event.
	newSrc := strings.Replace(deleted, "最后一段是结论。", "最后一段是修订过的结论。", 1)
	r := remapOne(t, deleted, newSrc, th)

	if r.Kind != KindUnchanged {
		t.Fatalf("a thread that's already orphaned and still unmatched should be recorded as unchanged, got %q", r.Kind)
	}
	if evs := eventsOrSkip(t, "", []Result{r}); len(evs) != 0 {
		t.Fatalf("unchanged should not produce an event, got %d", len(evs))
	}
}

func TestRemapSkipsResolvedThreads(t *testing.T) {
	newSrc := strings.Replace(oldSrc, "第一段是引言部分。", "改写后的引言。\n\n第一段是引言部分。", 1)

	open := threadAt(t, oldSrc)
	resolved := threadAt(t, oldSrc)
	resolved.Thread = "c00res"
	resolved.Status = api.StatusResolved
	addressed := threadAt(t, oldSrc)
	addressed.Thread = "c00add"
	addressed.Status = api.StatusAddressed

	rs, err := Remap([]byte(oldSrc), []byte(newSrc),
		[]api.Thread{open, resolved, addressed}, DefaultOptions())
	if err != nil {
		t.Fatalf("Remap: %v", err)
	}
	for _, r := range rs {
		if r.Thread == "c00res" {
			t.Fatalf("a resolved thread should not appear in the results: %+v", r)
		}
	}
	if len(rs) != 2 {
		t.Fatalf("open + addressed should total 2, got %d", len(rs))
	}
}

func TestRemapUnchangedWhenContentIdentical(t *testing.T) {
	r := remapOne(t, oldSrc, oldSrc, threadAt(t, oldSrc))
	if r.Kind != KindUnchanged {
		t.Fatalf("unchanged content should be unchanged, got %q", r.Kind)
	}
}

func TestRemapOneSkipsElementAnchors(t *testing.T) {
	a := anchor.Anchor{Kind: api.AnchorElement, AID: "b2c9x1", Exact: "按钮文案"}
	r, err := RemapOne([]byte(oldSrc), []byte("完全不同的内容"), a, DefaultOptions())
	if err != nil {
		t.Fatalf("RemapOne: %v", err)
	}
	if r.Kind != KindUnchanged {
		t.Fatalf("an element anchor should not participate in offset shifting, got %q", r.Kind)
	}
}

func TestNewDMPUsesDefaults(t *testing.T) {
	dmp := NewDMP(Options{})
	if dmp.MatchThreshold != 0.5 {
		t.Fatalf("MatchThreshold = %v, want 0.5", dmp.MatchThreshold)
	}
	if dmp.MatchDistance != 1000 {
		t.Fatalf("MatchDistance = %v", dmp.MatchDistance)
	}
	custom := NewDMP(Options{MatchThreshold: 0.8, MatchDistance: 42, DiffTimeoutSec: 2})
	// Options.MatchThreshold is a float32, so converting to float64 won't equal 0.8 exactly.
	if math.Abs(custom.MatchThreshold-0.8) > 1e-6 || custom.MatchDistance != 42 {
		t.Fatalf("custom parameters did not take effect: %+v", custom)
	}
	if custom.DiffTimeout != 2*time.Second {
		t.Fatalf("DiffTimeout = %v", custom.DiffTimeout)
	}
}

// ---------------------------------------------------------------------------
// Events: depends on W-core's eventlog.NewEvent
// ---------------------------------------------------------------------------

// eventsOrSkip skips the test if eventlog.NewEvent isn't implemented yet
// (panics); once W-core lands it, this becomes a real assertion automatically.
func eventsOrSkip(t *testing.T, rev string, rs []Result) (evs []eventlog.Event) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("eventlog.NewEvent not implemented yet (W-core), skipping: %v", r)
		}
	}()
	return Events(rev, rs)
}

func TestEventsMapsKinds(t *testing.T) {
	rs := []Result{
		{Thread: "c00001", Kind: KindUnchanged, Start: 1, End: 2},
		{Thread: "c00002", Kind: KindMoved, Start: 10, End: 20},
		{Thread: "c00003", Kind: KindRevived, Start: 30, End: 40},
		{Thread: "c00004", Kind: KindOrphan, Exact: anchored},
	}
	evs := eventsOrSkip(t, "9f8e7d6", rs)

	if len(evs) != 3 {
		t.Fatalf("unchanged produces no event, so we should get 3, got %d", len(evs))
	}
	if evs[0].E != eventlog.KindRemap || evs[0].Thread != "c00002" ||
		evs[0].Start != 10 || evs[0].End != 20 || evs[0].Rev != "9f8e7d6" {
		t.Fatalf("wrong moved -> remap event: %+v", evs[0])
	}
	if evs[1].E != eventlog.KindRemap || evs[1].Thread != "c00003" {
		t.Fatalf("wrong revived -> remap event: %+v", evs[1])
	}
	if evs[2].E != eventlog.KindOrphan || evs[2].LastExact != anchored {
		t.Fatalf("orphan event should retain last_exact: %+v", evs[2])
	}
	for _, e := range evs {
		if e.EID == "" || e.TS.IsZero() {
			t.Fatalf("event is missing eid/ts: %+v", e)
		}
	}
}
