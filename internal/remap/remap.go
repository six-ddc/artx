// Package remap uses diff-match-patch to carry anchors forward from an old
// version of a document's content to a new one.
//
// Owned by: W-anchor.
//
// Algorithm (design doc §7):
//  1. Run DiffMain over (oldSrc, newSrc) to get the diff sequence.
//  2. Shift each open thread's Start/End through the diff via DiffXIndex.
//  3. Validate against Anchor.Exact at the new position: a hit produces KindMoved.
//  4. On a miss, run bitap fuzzy search (MatchMain, threshold MatchThreshold)
//     using the shifted position as the expected location.
//  5. If that still misses, produce KindOrphan, retaining last_exact.
//
// Threads that are already resolved are skipped entirely. Threads that are
// already orphaned still participate — if the content comes back, they
// should revive automatically.
package remap

import (
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/six-ddc/artx/internal/anchor"
	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/eventlog"
)

// Result kinds.
const (
	KindUnchanged = "unchanged" // position unchanged, no event produced
	KindMoved     = "moved"     // position shifted, produces a remap event
	KindOrphan    = "orphan"    // anchor text vanished, produces an orphan event
	KindRevived   = "revived"   // was orphaned, now matched again, produces a remap event
)

// Options controls the strength of fuzzy matching.
type Options struct {
	// MatchThreshold is the bitap similarity threshold: 0 = exact match
	// only, 1 = anything passes. The design doc fixes this at 0.5.
	MatchThreshold float32
	// MatchDistance is the scale of the positional-distance penalty; larger
	// values tolerate matches farther from the expected position.
	MatchDistance int
	// DiffTimeoutSec is DiffMain's timeout in seconds; once it elapses,
	// DiffMain falls back to a line-level diff.
	DiffTimeoutSec float64
}

// DefaultOptions returns the defaults mandated by the design doc
// (MatchThreshold=0.5).
func DefaultOptions() Options {
	return Options{
		MatchThreshold: 0.5,
		MatchDistance:  1000,
		DiffTimeoutSec: 1,
	}
}

// NewDMP builds a diff-match-patch instance from opts.
// This is the package's single construction point for a DMP instance, to
// guarantee that diffing and bitap always share the same parameters.
func NewDMP(opts Options) *diffmatchpatch.DiffMatchPatch {
	def := DefaultOptions()
	dmp := diffmatchpatch.New()
	if opts.MatchThreshold > 0 {
		dmp.MatchThreshold = float64(opts.MatchThreshold)
	} else {
		dmp.MatchThreshold = float64(def.MatchThreshold)
	}
	if opts.MatchDistance > 0 {
		dmp.MatchDistance = opts.MatchDistance
	} else {
		dmp.MatchDistance = def.MatchDistance
	}
	sec := opts.DiffTimeoutSec
	if sec <= 0 {
		sec = def.DiffTimeoutSec
	}
	dmp.DiffTimeout = time.Duration(sec * float64(time.Second))
	return dmp
}

// Result is the remap outcome for a single thread.
type Result struct {
	Thread string
	Kind   string // one of the Kind* constants above
	Start  int    // new position; meaningless when Kind == KindOrphan
	End    int
	Score  float64 // 1.0 = exact, 0<s<1 = fuzzy
	Exact  string  // source text at the new position, used to refresh Anchor.Exact
	// When Kind == KindOrphan, Exact instead carries the anchor text as it
	// was **right before it vanished**; Events uses it to fill last_exact —
	// this is the only place last_exact ever comes from, since once the
	// orphan event is written the original text is no longer retrievable.
}

// Remap shifts a set of threads' anchors from oldSrc to newSrc.
// Threads whose Status is api.StatusResolved are skipped (they will not
// appear in the returned slice).
//
// A thread that was already orphaned and still doesn't match this round is
// recorded as KindUnchanged rather than KindOrphan: since its status hasn't
// actually changed, it shouldn't append another orphan event — otherwise
// every save would pile duplicate entries into the log.
func Remap(oldSrc, newSrc []byte, threads []api.Thread, opts Options) ([]Result, error) {
	var out []Result
	for i := range threads {
		t := &threads[i]
		if t.Status == api.StatusResolved {
			continue
		}
		a := fromAPI(t.Anchor)
		r, err := RemapOne(oldSrc, newSrc, a, opts)
		if err != nil {
			return nil, err
		}
		r.Thread = t.Thread
		switch {
		case t.Anchor.Orphan && r.Kind == KindOrphan:
			r.Kind = KindUnchanged
		case t.Anchor.Orphan:
			r.Kind = KindRevived
		}
		out = append(out, r)
	}
	return out, nil
}

// RemapOne shifts a single anchor.
func RemapOne(oldSrc, newSrc []byte, a anchor.Anchor, opts Options) (Result, error) {
	// Element anchors aren't positioned by byte offset, so they never need
	// to be shifted regardless of content changes.
	if a.Kind == api.AnchorElement {
		return Result{Kind: KindUnchanged, Start: a.Start, End: a.End, Score: 1, Exact: a.Exact}, nil
	}
	if a.Exact == "" {
		return Result{Kind: KindUnchanged, Start: a.Start, End: a.End, Exact: a.Exact}, nil
	}

	dmp := NewDMP(opts)
	diffs := dmp.DiffMain(string(oldSrc), string(newSrc), true)

	// DiffXIndex accumulates over len(Diff.Text), i.e. it counts **bytes**,
	// matching the unit our offsets use.
	ns := dmp.DiffXIndex(diffs, a.Start)
	ne := dmp.DiffXIndex(diffs, a.End)

	probe := a
	probe.Start, probe.End = ns, ne
	m, err := anchor.Locate(newSrc, probe)
	if err != nil {
		return Result{Kind: KindOrphan, Score: 0, Exact: a.Exact}, nil
	}

	kind := KindMoved
	if m.Start == a.Start && m.End == a.End {
		kind = KindUnchanged
	}
	return Result{
		Kind:  kind,
		Start: m.Start,
		End:   m.End,
		Score: m.Score,
		Exact: string(newSrc[m.Start:m.End]),
	}, nil
}

// Events converts remap results into events to be appended to the log.
// KindUnchanged produces no event. rev is the git sha corresponding to the
// new content (pass an empty string if it hasn't been committed yet).
func Events(rev string, results []Result) []eventlog.Event {
	var out []eventlog.Event
	for _, r := range results {
		switch r.Kind {
		case KindMoved, KindRevived:
			e := eventlog.NewEvent(eventlog.KindRemap)
			e.Thread = r.Thread
			e.Start = r.Start
			e.End = r.End
			e.Rev = rev
			out = append(out, e)
		case KindOrphan:
			e := eventlog.NewEvent(eventlog.KindOrphan)
			e.Thread = r.Thread
			e.LastExact = r.Exact
			e.Rev = rev
			out = append(out, e)
		}
	}
	return out
}

// fromAPI converts a presentation-form anchor back into its persisted form.
// An orphaned thread's Exact may already be empty, in which case LastExact
// is used as the search pattern instead — revival detection depends
// entirely on it.
func fromAPI(t api.ThreadAnchor) anchor.Anchor {
	exact := t.Exact
	if exact == "" {
		exact = t.LastExact
	}
	return anchor.Anchor{
		Kind:   t.Kind,
		Exact:  exact,
		Prefix: t.Prefix,
		Suffix: t.Suffix,
		Start:  t.Start,
		End:    t.End,
		Rev:    t.Rev,
		AID:    t.AID,
		Approx: t.Approx,
	}
}
