package eventlog

import (
	"fmt"

	"github.com/six-ddc/artx/internal/api"
)

// CompactMessage renders the commit subject for a compaction run, shared by
// the CLI and the serve endpoint so the format cannot drift apart, e.g.
// "artx: compact a7f3 (120 -> 8 events, 3 threads archived)". Skipped stats
// are excluded: they produced no changes, so they are not part of the commit.
func CompactMessage(stats []api.CompactStat) string {
	var done []api.CompactStat
	for _, s := range stats {
		if !s.Skipped {
			done = append(done, s)
		}
	}

	target := "vault"
	switch len(done) {
	case 0:
		// Nothing was compacted; Commit will see no staged changes and
		// return without committing, so the message is never used.
		return "artx: compact " + target
	case 1:
		target = done[0].Doc
	default:
		target = fmt.Sprintf("%d docs", len(done))
	}

	before, after, archived := 0, 0, 0
	for _, s := range done {
		before += s.EventsBefore
		after += s.EventsAfter
		archived += s.ThreadsArchived
	}

	msg := fmt.Sprintf("artx: compact %s (%d -> %d events", target, before, after)
	if archived > 0 {
		threads := "threads"
		if archived == 1 {
			threads = "thread"
		}
		msg += fmt.Sprintf(", %d %s archived", archived, threads)
	}
	return msg + ")"
}
