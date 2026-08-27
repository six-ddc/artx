// Package idgen generates the various identifiers used by art.
//
// Owner: W-core.
//
// All ids are random base36 (design doc §13, decision 1). Thread and reply
// ids in particular must be random rather than incrementing counters:
// when two machines each append comments to the same document and those
// changes converge through git's merge=union, incrementing counters are
// bound to collide, whereas random ids won't.
package idgen

import (
	"crypto/rand"
	"encoding/binary"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Alphabet is the base36 character set: 0-9a-z.
const Alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// Lengths (in characters) of each kind of id.
const (
	DocIDLen     = 6 // doc id, e.g. a7f3k2
	ElementIDLen = 6 // html data-aid
	ThreadIDLen  = 5 // random part of a thread id; with the "c" prefix, 6 chars total, e.g. c7k2f9
	ReplyIDLen   = 3 // random part of a reply id, shaped like c7k2f9.x8q
	EventSuffix  = 4 // random suffix of an event id
)

// ThreadPrefix is the fixed prefix on thread ids, letting a CLI argument be
// told apart from a doc id at a glance.
const ThreadPrefix = "c"

// DocID returns a 6-character random base36 doc id.
func DocID() string { return Random(DocIDLen) }

// ElementID returns a 6-character random base36 element id, used for an
// html element's data-aid.
func ElementID() string { return Random(ElementIDLen) }

// ThreadID returns a thread id shaped like c7k2f9.
func ThreadID() string { return ThreadPrefix + Random(ThreadIDLen) }

// ReplyID returns a reply id shaped like <thread>.x8q.
func ReplyID(threadID string) string { return threadID + "." + Random(ReplyIDLen) }

// eventSuffixSpace is the number of distinct EventSuffix-character base36
// strings: 36^EventSuffix.
var eventSuffixSpace = func() uint32 {
	n := uint32(1)
	for i := 0; i < EventSuffix; i++ {
		n *= uint32(len(Alphabet))
	}
	return n
}()

var (
	eventIDMu     sync.Mutex
	eventIDLastMs int64
	eventIDNext   uint32
)

// EventID returns an event id shaped like <base36(unixMilli)>-<4 random
// chars>. It doubles as the event's dedup key: git's merge=union can cause
// the same event block to appear twice, and folding dedups by this id.
//
// Within one process, uniqueness inside a single millisecond is guaranteed
// **by construction**, not just probabilistically. A plain independent
// crypto/rand draw for the 4-character suffix collides with real,
// non-negligible probability at realistic issuance rates (~26% at 1000 ids
// in the same millisecond, by the birthday bound) — and because EventID is
// the fold dedup key, a collision silently drops one of the two colliding
// events. That burst case is single-process (a watcher pass appending many
// remap events), which is what the guarantee below covers. Two separate
// processes issuing in the same millisecond — or two machines merging via
// merge=union — still rely on the random starting points not overlapping,
// which at their far lower per-millisecond rates is a negligible risk
// rather than the routine one above. To close the in-process gap: the
// first call in a given millisecond picks a random starting point
// in the suffix space (36^EventSuffix = 1,679,616 values), and every
// subsequent call within that same millisecond advances to the next slot
// (wrapping if the space is ever exhausted, which realistic call volumes
// never approach). Crossing into a new millisecond resets to a fresh
// random starting point, so ids are not sequentially guessable across
// milliseconds. This function is safe for concurrent use — it may be
// called both from a serve Writer goroutine and directly from the CLI.
func EventID() string {
	eventIDMu.Lock()
	ms := time.Now().UnixMilli()
	if ms != eventIDLastMs {
		eventIDLastMs = ms
		eventIDNext = randomSuffixStart()
	}
	idx := eventIDNext
	eventIDNext = (eventIDNext + 1) % eventSuffixSpace
	eventIDMu.Unlock()

	return strconv.FormatInt(ms, 36) + "-" + encodeSuffix(idx)
}

// randomSuffixStart draws a uniformly random starting index into the
// EventSuffix-character suffix space, using crypto/rand.
func randomSuffixStart() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return binary.BigEndian.Uint32(b[:]) % eventSuffixSpace
}

// encodeSuffix renders idx as an EventSuffix-character base36 string,
// zero-padded on the left (i.e. using Alphabet's first character as filler).
func encodeSuffix(idx uint32) string {
	base := uint32(len(Alphabet))
	buf := make([]byte, EventSuffix)
	for i := EventSuffix - 1; i >= 0; i-- {
		buf[i] = Alphabet[idx%base]
		idx /= base
	}
	return string(buf)
}

// IsThreadID reports whether s has the shape of a thread id.
func IsThreadID(s string) bool {
	if len(s) != len(ThreadPrefix)+ThreadIDLen {
		return false
	}
	if !strings.HasPrefix(s, ThreadPrefix) {
		return false
	}
	return isBase36(s[len(ThreadPrefix):])
}

// IsReplyID reports whether s has the shape of a reply id.
func IsReplyID(s string) bool {
	i := strings.LastIndexByte(s, '.')
	if i < 0 {
		return false
	}
	thread, suffix := s[:i], s[i+1:]
	if len(suffix) != ReplyIDLen || !isBase36(suffix) {
		return false
	}
	return IsThreadID(thread)
}

// ThreadOfComment extracts the owning thread id from a comment id (which may
// itself be a thread id or a reply id).
func ThreadOfComment(id string) string {
	if i := strings.LastIndexByte(id, '.'); i >= 0 {
		return id[:i]
	}
	return id
}

// Random returns an n-character random base36 string, using crypto/rand.
func Random(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, n)
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	base := byte(len(Alphabet))
	for i, b := range buf {
		out[i] = Alphabet[b%base]
	}
	return string(out)
}

func isBase36(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune(Alphabet, c) {
			return false
		}
	}
	return true
}
