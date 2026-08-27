// Package eventlog implements the append-only YAML event stream: reading,
// writing, folding, and compaction.
//
// Owned by W-core. Event, the Kind constants, and FoldResult are a **frozen
// contract**: both W-anchor (emits remap/orphan) and W-serve (emits browser
// events) depend on them, so their fields and tags must not change.
//
// File format: <vault>/.art/comments/<docid>.yaml, a multi-document YAML
// stream where each event is a block starting with "---". Writers are only
// allowed to **append complete blocks at the end of the file**; rewriting
// existing lines is a privilege reserved for Compact.
package eventlog

import (
	"errors"
	"time"

	"github.com/six-ddc/art/internal/anchor"
	"github.com/six-ddc/art/internal/idgen"
)

// Event kinds. Corresponds to the `e:` field in YAML.
const (
	KindCreate    = "create"    // creates a new thread
	KindReply     = "reply"     // adds a reply within a thread
	KindEdit      = "edit"      // edits a comment body; carries the full new body, overwritten on fold
	KindAddressed = "addressed" // an agent declares the thread handled
	KindResolve   = "resolve"   // a human confirms the thread closed
	KindReopen    = "reopen"    // reopens a closed thread
	KindRemap     = "remap"     // emitted by the watcher: anchor offsets shifted
	KindOrphan    = "orphan"    // emitted by the watcher: anchor text disappeared
	KindArchive   = "archive"   // folded summary written by compact into the archive file
)

// ErrCorruptTail indicates the event stream's tail contains a block that
// could not be parsed. Because the stream is append-only, damage can only
// ever occur at the tail; `art doctor` is responsible for trimming it.
var ErrCorruptTail = errors.New("eventlog: corrupt tail block")

// ErrThreadNotFound indicates a reference to a thread that does not exist.
var ErrThreadNotFound = errors.New("eventlog: thread not found")

// Event is a single block in the event stream.
//
// It deliberately uses **one flat struct with omitempty** rather than a
// distinct type per event kind:
//   - the YAML stream is heterogeneous, and a flat struct lets decoding
//     happen in one pass, without first peeking at `e` to pick a type;
//   - omitempty keeps each written block limited to the fields that are
//     meaningful for that event kind, so `git diff` stays readable;
//   - unknown fields are safely ignored, so adding a field later won't break
//     parsing in older versions of art.
//
// Field applicability by kind (blueprint.md has the full reference table):
//
//	create    : eid ts thread author body anchor rev
//	reply     : eid ts thread id author body
//	edit      : eid ts target author body
//	addressed : eid ts thread by commit note
//	resolve   : eid ts thread by
//	reopen    : eid ts thread by note
//	remap     : eid ts thread start end rev
//	orphan    : eid ts thread last_exact rev
//	archive   : eid ts thread (compact summary, only appears in .archive.yaml)
type Event struct {
	E   string    `yaml:"e"`             // event kind, see the constants above
	EID string    `yaml:"eid,omitempty"` // event id, the dedup key; git's merge=union can produce duplicate blocks
	TS  time.Time `yaml:"ts,omitempty"`  // RFC3339, with timezone

	Thread string `yaml:"thread,omitempty"` // target thread
	ID     string `yaml:"id,omitempty"`     // reply: id of this reply comment
	Target string `yaml:"target,omitempty"` // edit: id of the comment being edited (thread id or reply id)

	Author string `yaml:"author,omitempty"` // author of create/reply/edit
	By     string `yaml:"by,omitempty"`     // actor for addressed/resolve/reopen
	Body   string `yaml:"body,omitempty"`

	Anchor *anchor.Anchor `yaml:"anchor,omitempty"` // create only

	Start int    `yaml:"start,omitempty"` // remap only
	End   int    `yaml:"end,omitempty"`   // remap only
	Rev   string `yaml:"rev,omitempty"`   // create/remap/orphan: the corresponding git sha

	Commit string `yaml:"commit,omitempty"` // addressed only
	Note   string `yaml:"note,omitempty"`   // addressed/reopen

	LastExact string `yaml:"last_exact,omitempty"` // orphan only

	// Archived only appears on archive events in .archive.yaml; it carries
	// the full snapshot of the folded thread.
	Archived *ArchivedThread `yaml:"archived,omitempty"`
}

// ArchivedThread is the snapshot compact writes to the archive file after
// folding a resolved thread away.
type ArchivedThread struct {
	Thread     string         `yaml:"thread"`
	Author     string         `yaml:"author"`
	Body       string         `yaml:"body"`
	Anchor     *anchor.Anchor `yaml:"anchor,omitempty"`
	Replies    []ArchivedNote `yaml:"replies,omitempty"`
	CreatedAt  time.Time      `yaml:"created_at"`
	ResolvedAt time.Time      `yaml:"resolved_at"`
	ResolvedBy string         `yaml:"resolved_by,omitempty"`
	Events     int            `yaml:"events"` // number of raw events folded into this snapshot
}

// ArchivedNote is a single reply inside an archived thread snapshot.
type ArchivedNote struct {
	ID     string    `yaml:"id"`
	Author string    `yaml:"author"`
	Body   string    `yaml:"body"`
	TS     time.Time `yaml:"ts"`
}

// StatusKinds lists the event kinds that change a thread's status; fold
// applies the last one in sorted order to determine the current status.
var StatusKinds = map[string]string{
	KindCreate:    "open",
	KindAddressed: "addressed",
	KindResolve:   "resolved",
	KindReopen:    "open",
}

// NewEvent builds an event with EID and TS already filled in.
func NewEvent(kind string) Event {
	return Event{
		E:   kind,
		EID: idgen.EventID(),
		TS:  time.Now(),
	}
}
