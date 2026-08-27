package eventlog

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/six-ddc/art/internal/anchor"
	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/idgen"
	"github.com/six-ddc/art/internal/lockfile"
)

// CommentsDir is the event log directory, relative to the vault root.
const CommentsDir = ".art/comments"

// Compaction trigger thresholds (design doc §6.3); can be overridden by
// vault configuration.
const (
	DefaultCompactSizeBytes   = 256 * 1024
	DefaultCompactResolvedAge = 30 * 24 * time.Hour
)

// archiveSuffix is the suffix appended to the active file's name to get its
// archive file name.
const archiveSuffix = ".archive.yaml"

// Store reads and writes all event logs under a given vault.
type Store struct {
	root string
}

// Open returns a Store bound to the vault root. Performs no IO.
func Open(root string) *Store { return &Store{root: root} }

// Root returns the vault's absolute path.
func (s *Store) Root() string { return s.root }

// Path returns the absolute path of a document's active event log.
func (s *Store) Path(docID string) string {
	return filepath.Join(s.root, CommentsDir, docID+".yaml")
}

// ArchivePath returns the absolute path of a document's archive file.
func (s *Store) ArchivePath(docID string) string {
	return filepath.Join(s.root, CommentsDir, docID+archiveSuffix)
}

// DocIDs lists the ids of all documents that have an event log under
// .art/comments.
func (s *Store) DocIDs() ([]string, error) {
	dir := filepath.Join(s.root, CommentsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, archiveSuffix) {
			continue
		}
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".yaml"))
	}
	sort.Strings(ids)
	return ids, nil
}

// ReadReport describes any anomaly encountered during a read.
type ReadReport struct {
	Events      int      // number of events successfully parsed
	TailCorrupt bool     // the tail contains a block that could not be parsed
	TailOffset  int64    // byte offset where the corrupt block starts; art doctor truncates from here
	Warnings    []string // human-readable warnings
}

// Read streams and parses all events for a document.
//
// Fault-tolerance contract: the decoder stops at the first error, returns
// every event parsed so far, and flags TailCorrupt in the report. This is
// consistent with append-only semantics — damage can only occur at the
// tail, so everything before it remains valid. Because of that, Read does
// **not** return an error on tail corruption; callers inspect the report to
// decide whether to warn.
func (s *Store) Read(docID string) ([]Event, *ReadReport, error) {
	f, err := os.Open(s.Path(docID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ReadReport{}, nil
		}
		return nil, nil, err
	}
	defer f.Close()
	return ReadFrom(f)
}

// rawBlock is a single event block, delimited by a "---" line, in the raw
// byte stream.
type rawBlock struct {
	offset int // byte offset where the "---" marker line itself starts
	body   []byte
}

// splitRawBlocks splits the whole file into blocks along "---" delimiter
// lines. Any content before the first marker (normally none) is ignored.
func splitRawBlocks(data []byte) []rawBlock {
	var markers []int
	off := 0
	for _, line := range splitLinesKeepEnds(data) {
		trimmed := strings.TrimSpace(strings.TrimRight(string(line), "\r\n"))
		if trimmed == "---" {
			markers = append(markers, off)
		}
		off += len(line)
	}
	blocks := make([]rawBlock, 0, len(markers))
	for i, m := range markers {
		lineEnd := bytes.IndexByte(data[m:], '\n')
		bodyStart := len(data)
		if lineEnd >= 0 {
			bodyStart = m + lineEnd + 1
		}
		bodyEnd := len(data)
		if i+1 < len(markers) {
			bodyEnd = markers[i+1]
		}
		blocks = append(blocks, rawBlock{offset: m, body: data[bodyStart:bodyEnd]})
	}
	return blocks
}

func splitLinesKeepEnds(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			lines = append(lines, data[start:i+1])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

// ReadFrom parses an event stream from any reader; shared by tests and by
// archive-file reading.
func ReadFrom(r io.Reader) ([]Event, *ReadReport, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}
	report := &ReadReport{}
	var events []Event
	for _, b := range splitRawBlocks(data) {
		if len(bytes.TrimSpace(b.body)) == 0 {
			// A blank trailing block indicates a writer that died right
			// after emitting the "---\n" marker, before the YAML body.
			report.TailCorrupt = true
			report.TailOffset = int64(b.offset)
			report.Warnings = append(report.Warnings, "eventlog: empty trailing block")
			break
		}
		var e Event
		if err := yaml.Unmarshal(b.body, &e); err != nil {
			report.TailCorrupt = true
			report.TailOffset = int64(b.offset)
			report.Warnings = append(report.Warnings, fmt.Sprintf("eventlog: corrupt block at offset %d: %v", b.offset, err))
			break
		}
		events = append(events, e)
	}
	report.Events = len(events)
	return events, report, nil
}

// Append appends one or more complete event blocks to the end of the file.
//
// Requirements:
//   - holds an flock (lockfile.Acquire) on the target yaml file for the
//     whole operation
//   - serializes all events into an in-memory buffer first, then does a
//     single Write, to avoid partial writes
//   - each block starts with "---\n"
//   - creates the directory if it doesn't exist
//   - fills in EID/TS for any event that doesn't already have them
func (s *Store) Append(docID string, events ...Event) error {
	if len(events) == 0 {
		return nil
	}
	path := s.Path(docID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	now := time.Now()
	filled := make([]Event, len(events))
	copy(filled, events)
	for i := range filled {
		if filled[i].EID == "" {
			filled[i].EID = idgen.EventID()
		}
		if filled[i].TS.IsZero() {
			filled[i].TS = now
		}
	}

	buf, err := Marshal(filled...)
	if err != nil {
		return err
	}

	lock, err := lockfile.Acquire(path, 0o644, 0)
	if err != nil {
		return err
	}
	defer lock.Unlock()

	f := lock.File()
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	if _, err := f.Write(buf); err != nil {
		return err
	}
	return f.Sync()
}

// Marshal serializes events into blocks ready to append, each with a
// leading "---\n".
func Marshal(events ...Event) ([]byte, error) {
	var buf bytes.Buffer
	for _, e := range events {
		buf.WriteString("---\n")
		data, err := yaml.Marshal(e)
		if err != nil {
			return nil, err
		}
		buf.Write(data)
		if len(data) == 0 || data[len(data)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), nil
}

// FoldResult is the result of folding the full event stream.
type FoldResult struct {
	Threads  []api.Thread // ascending by CreatedAt
	Warnings []string     // orphaned references, unknown event kinds, etc.
}

// Fold collapses an event sequence into the current state of each thread.
//
// Rules (any two independent implementations must match exactly on every
// point; blueprint.md has the same table):
//  1. Dedup by EID first (git's merge=union can produce fully duplicate
//     blocks).
//  2. Then **stable-sort** ascending by ts, and by eid when ts ties.
//     Physical file order plays no part, since it is not trustworthy after
//     a union merge.
//  3. create starts a thread; if the same thread has multiple create
//     events, the first one in sorted order wins.
//  4. reply appends a comment, deduped by id.
//  5. edit overwrites the target's body and records EditedAt; when target
//     is the thread id, it edits the root comment.
//  6. addressed/resolve/reopen set Status from the last one in sorted
//     order, per StatusKinds.
//  7. remap takes the last one, overwrites Anchor.Start/End/Rev, and clears
//     Orphan.
//  8. orphan sets Orphan=true and LastExact; a later remap clears it again.
//  9. Events referencing a nonexistent thread are dropped and recorded in
//     Warnings; same for unrecognized e values.
//  10. Thread.UpdatedAt is the maximum ts among all of that thread's
//     events.
//
// In the returned Thread, Doc/Slug/Path are left empty and
// Anchor.Line/Context are left empty; the caller and anchor.Enrich fill
// those in respectively.
func Fold(events []Event) *FoldResult {
	result := &FoldResult{}

	// 1. Dedup by eid. Events without an eid are never considered
	// duplicates of one another (there is no key to compare).
	seen := make(map[string]bool, len(events))
	deduped := make([]Event, 0, len(events))
	for _, e := range events {
		if e.EID != "" {
			if seen[e.EID] {
				continue
			}
			seen[e.EID] = true
		}
		deduped = append(deduped, e)
	}

	// 2. Stable sort by (ts, eid) ascending. Physical file order is not
	// trusted after a merge=union union. A zero time.Time sorts first,
	// which matches the intent that "events with no ts sort to the front".
	sort.SliceStable(deduped, func(i, j int) bool {
		a, b := deduped[i], deduped[j]
		if !a.TS.Equal(b.TS) {
			return a.TS.Before(b.TS)
		}
		return a.EID < b.EID
	})

	threads := make(map[string]*api.Thread)
	replySeen := make(map[string]map[string]bool)
	order := make([]string, 0)

	warn := func(format string, args ...any) {
		result.Warnings = append(result.Warnings, fmt.Sprintf(format, args...))
	}

	touch := func(th *api.Thread, ts time.Time) {
		if ts.After(th.UpdatedAt) {
			th.UpdatedAt = ts
		}
	}

	for _, e := range deduped {
		switch e.E {
		case KindCreate:
			if e.Thread == "" {
				warn("create event %s missing thread id", e.EID)
				continue
			}
			if _, exists := threads[e.Thread]; exists {
				warn("duplicate create for thread %s (eid=%s)", e.Thread, e.EID)
				continue
			}
			th := &api.Thread{
				Thread: e.Thread,
				Status: api.StatusOpen,
				Author: e.Author,
				Body:   e.Body,
				// Replies has no `omitempty` tag in the frozen api.Thread
				// contract, so a nil slice here would marshal as JSON
				// `null` instead of `[]` and break consumers (TS types,
				// frontend rendering) that expect an array. Always start
				// from an empty, non-nil slice.
				Replies:    []api.Reply{},
				CreatedAt:  e.TS,
				UpdatedAt:  e.TS,
				CreatedRev: e.Rev,
			}
			if e.Anchor != nil {
				th.Anchor = anchor.ToAPI(*e.Anchor)
			} else {
				warn("create event for thread %s missing anchor", e.Thread)
			}
			threads[e.Thread] = th
			replySeen[e.Thread] = map[string]bool{}
			order = append(order, e.Thread)

		case KindReply:
			th, ok := threads[e.Thread]
			if !ok {
				warn("reply %s references unknown thread %s", e.ID, e.Thread)
				continue
			}
			if e.ID == "" {
				warn("reply event %s missing id", e.EID)
				continue
			}
			if replySeen[e.Thread][e.ID] {
				warn("duplicate reply %s", e.ID)
				continue
			}
			replySeen[e.Thread][e.ID] = true
			th.Replies = append(th.Replies, api.Reply{
				ID:        e.ID,
				Author:    e.Author,
				Body:      e.Body,
				CreatedAt: e.TS,
			})
			touch(th, e.TS)

		case KindEdit:
			if e.Target == "" {
				warn("edit event %s missing target", e.EID)
				continue
			}
			threadID := idgen.ThreadOfComment(e.Target)
			th, ok := threads[threadID]
			if !ok {
				warn("edit %s references unknown thread %s", e.EID, threadID)
				continue
			}
			ts := e.TS
			if e.Target == th.Thread {
				th.Body = e.Body
				th.EditedAt = &ts
			} else {
				found := false
				for i := range th.Replies {
					if th.Replies[i].ID == e.Target {
						th.Replies[i].Body = e.Body
						th.Replies[i].EditedAt = &ts
						found = true
						break
					}
				}
				if !found {
					warn("edit %s references unknown reply %s", e.EID, e.Target)
					continue
				}
			}
			touch(th, e.TS)

		case KindAddressed, KindResolve, KindReopen:
			th, ok := threads[e.Thread]
			if !ok {
				warn("%s event references unknown thread %s", e.E, e.Thread)
				continue
			}
			th.Status = StatusKinds[e.E]
			if e.E == KindAddressed {
				th.Addressed = &api.Addressed{By: e.By, Commit: e.Commit, Note: e.Note, At: e.TS}
			}
			touch(th, e.TS)

		case KindRemap:
			th, ok := threads[e.Thread]
			if !ok {
				warn("remap event references unknown thread %s", e.Thread)
				continue
			}
			th.Anchor.Start = e.Start
			th.Anchor.End = e.End
			if e.Rev != "" {
				th.Anchor.Rev = e.Rev
			}
			// A live remap means the anchor text was found again: clear
			// any orphan state so the thread "revives".
			th.Anchor.Orphan = false
			th.Anchor.LastExact = ""
			th.Hint = ""
			touch(th, e.TS)

		case KindOrphan:
			th, ok := threads[e.Thread]
			if !ok {
				warn("orphan event references unknown thread %s", e.Thread)
				continue
			}
			th.Anchor.Orphan = true
			th.Anchor.LastExact = e.LastExact
			th.Hint = api.OrphanHint
			touch(th, e.TS)

		case KindArchive:
			// Archive events are summaries written to the .archive.yaml
			// file; they never appear as live threads.
			continue

		default:
			warn("unknown event type %q (eid=%s)", e.E, e.EID)
		}
	}

	result.Threads = make([]api.Thread, 0, len(order))
	for _, id := range order {
		result.Threads = append(result.Threads, *threads[id])
	}
	sort.SliceStable(result.Threads, func(i, j int) bool {
		a, b := result.Threads[i], result.Threads[j]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.Thread < b.Thread
	})
	return result
}

// Threads combines Read and Fold.
func (s *Store) Threads(docID string) (*FoldResult, error) {
	events, report, err := s.Read(docID)
	if err != nil {
		return nil, err
	}
	fr := Fold(events)
	if report != nil {
		fr.Warnings = append(fr.Warnings, report.Warnings...)
	}
	return fr, nil
}

// Truncate cuts the event log down to its first keep events, used by
// art doctor to trim a corrupt tail block. Runs under flock, since it
// rewrites the file; only doctor and compact are allowed to call it.
func (s *Store) Truncate(docID string, keep int) error {
	path := s.Path(docID)
	return lockfile.WithLock(path, 0o644, 0, func(f *os.File) error {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		events, _, err := ReadFrom(f)
		if err != nil {
			return err
		}
		if keep < 0 {
			keep = 0
		}
		if keep > len(events) {
			keep = len(events)
		}
		buf, err := Marshal(events[:keep]...)
		if err != nil {
			return err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, buf, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	})
}

// CompactOptions controls compaction behavior.
type CompactOptions struct {
	Force       bool          // ignore the thresholds
	SizeBytes   int64         // 0 means use DefaultCompactSizeBytes
	ResolvedAge time.Duration // 0 means use DefaultCompactResolvedAge
	Now         time.Time     // zero value means time.Now()
}

// eventThreadID returns the thread id an event belongs to; for edit events
// it is looked up via the target.
func eventThreadID(e Event) string {
	if e.E == KindEdit {
		return idgen.ThreadOfComment(e.Target)
	}
	return e.Thread
}

// Compact compacts a document's event log (design doc §6.3).
//
// Actions, all performed under flock:
//  1. Threads that are resolved and whose resolve time is older than
//     ResolvedAge are folded as a whole into a single KindArchive event,
//     appended to <docid>.archive.yaml, and removed from the active file.
//  2. For remaining threads, edit chains are collapsed into the
//     create/reply body.
//  3. remap chains are collapsed into create's anchor (keeping the last
//     event's start/end/rev).
//  4. The active file is atomically rewritten: write <docid>.yaml.tmp,
//     then rename.
//
// The git commit is the caller's responsibility (the art compact CLI
// command and serve each commit on their own).
func (s *Store) Compact(docID string, opts CompactOptions) (api.CompactStat, error) {
	stat := api.CompactStat{Doc: docID}
	path := s.Path(docID)

	err := lockfile.WithLock(path, 0o644, 0, func(f *os.File) error {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		stat.BytesBefore = info.Size()

		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		events, _, err := ReadFrom(f)
		if err != nil {
			return err
		}
		stat.EventsBefore = len(events)

		now := opts.Now
		if now.IsZero() {
			now = time.Now()
		}
		sizeThreshold := opts.SizeBytes
		if sizeThreshold <= 0 {
			sizeThreshold = DefaultCompactSizeBytes
		}
		resolvedAge := opts.ResolvedAge
		if resolvedAge <= 0 {
			resolvedAge = DefaultCompactResolvedAge
		}

		fold := Fold(events)
		threadByID := make(map[string]api.Thread, len(fold.Threads))
		for _, th := range fold.Threads {
			threadByID[th.Thread] = th
		}

		archiveSet := map[string]bool{}
		for _, th := range fold.Threads {
			if th.Status == api.StatusResolved && now.Sub(th.UpdatedAt) >= resolvedAge {
				archiveSet[th.Thread] = true
			}
		}

		needs := opts.Force || stat.BytesBefore >= sizeThreshold || len(archiveSet) > 0
		if !needs {
			stat.Skipped = true
			stat.EventsAfter = stat.EventsBefore
			stat.BytesAfter = stat.BytesBefore
			return nil
		}
		stat.ThreadsArchived = len(archiveSet)

		resolvedBy := map[string]string{}
		eventCount := map[string]int{}
		for _, e := range events {
			tid := eventThreadID(e)
			if tid == "" {
				continue
			}
			eventCount[tid]++
			if e.E == KindResolve {
				resolvedBy[e.Thread] = e.By
			}
		}

		var archiveEvents []Event
		for _, th := range fold.Threads {
			if !archiveSet[th.Thread] {
				continue
			}
			a := &anchor.Anchor{
				Kind:   th.Anchor.Kind,
				Exact:  th.Anchor.Exact,
				Prefix: th.Anchor.Prefix,
				Suffix: th.Anchor.Suffix,
				Start:  th.Anchor.Start,
				End:    th.Anchor.End,
				Rev:    th.Anchor.Rev,
				AID:    th.Anchor.AID,
				Approx: th.Anchor.Approx,
			}
			notes := make([]ArchivedNote, 0, len(th.Replies))
			for _, r := range th.Replies {
				notes = append(notes, ArchivedNote{ID: r.ID, Author: r.Author, Body: r.Body, TS: r.CreatedAt})
			}
			ae := NewEvent(KindArchive)
			ae.Thread = th.Thread
			ae.Archived = &ArchivedThread{
				Thread:     th.Thread,
				Author:     th.Author,
				Body:       th.Body,
				Anchor:     a,
				Replies:    notes,
				CreatedAt:  th.CreatedAt,
				ResolvedAt: th.UpdatedAt,
				ResolvedBy: resolvedBy[th.Thread],
				Events:     eventCount[th.Thread],
			}
			archiveEvents = append(archiveEvents, ae)
		}

		if len(archiveEvents) > 0 {
			archiveBuf, err := Marshal(archiveEvents...)
			if err != nil {
				return err
			}
			archivePath := s.ArchivePath(docID)
			if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
				return err
			}
			af, err := os.OpenFile(archivePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return err
			}
			_, werr := af.Write(archiveBuf)
			cerr := af.Close()
			if werr != nil {
				return werr
			}
			if cerr != nil {
				return cerr
			}
		}

		// Final body per comment id (thread id or reply id), for kept
		// threads only — this is the edit-chain collapse.
		finalBody := map[string]string{}
		for _, th := range fold.Threads {
			if archiveSet[th.Thread] {
				continue
			}
			finalBody[th.Thread] = th.Body
			for _, r := range th.Replies {
				finalBody[r.ID] = r.Body
			}
		}

		kept := make([]Event, 0, len(events))
		for _, e := range events {
			tid := eventThreadID(e)
			if tid == "" {
				continue // garbage referencing no thread
			}
			if _, known := threadByID[tid]; !known {
				continue // references a thread Fold already discarded
			}
			if archiveSet[tid] {
				continue // captured in the archive event above
			}
			switch e.E {
			case KindEdit:
				continue // collapsed into body, drop the edit event itself
			case KindRemap:
				continue // collapsed into create's anchor below
			case KindCreate:
				if body, ok := finalBody[e.Thread]; ok {
					e.Body = body
				}
				if th, ok := threadByID[e.Thread]; ok {
					if e.Anchor == nil {
						e.Anchor = &anchor.Anchor{}
					}
					e.Anchor.Start = th.Anchor.Start
					e.Anchor.End = th.Anchor.End
					if th.Anchor.Rev != "" {
						e.Anchor.Rev = th.Anchor.Rev
					}
				}
			case KindReply:
				if body, ok := finalBody[e.ID]; ok {
					e.Body = body
				}
			}
			kept = append(kept, e)
		}

		buf, err := Marshal(kept...)
		if err != nil {
			return err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, buf, 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			return err
		}

		stat.EventsAfter = len(kept)
		stat.BytesAfter = int64(len(buf))
		return nil
	})

	return stat, err
}

// NeedsCompact reports whether a document has reached the compaction
// threshold; called periodically by serve.
func (s *Store) NeedsCompact(docID string, opts CompactOptions) (bool, error) {
	info, err := os.Stat(s.Path(docID))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	sizeThreshold := opts.SizeBytes
	if sizeThreshold <= 0 {
		sizeThreshold = DefaultCompactSizeBytes
	}
	if info.Size() >= sizeThreshold {
		return true, nil
	}

	resolvedAge := opts.ResolvedAge
	if resolvedAge <= 0 {
		resolvedAge = DefaultCompactResolvedAge
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	fold, err := s.Threads(docID)
	if err != nil {
		return false, err
	}
	for _, th := range fold.Threads {
		if th.Status == api.StatusResolved && now.Sub(th.UpdatedAt) >= resolvedAge {
			return true, nil
		}
	}
	return false, nil
}
