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
	"strconv"
	"strings"
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

// EventID returns an event id shaped like <base36(unixMilli)>-<4 random
// chars>. It doubles as the event's dedup key: git's merge=union can cause
// the same event block to appear twice, and folding dedups by this id.
func EventID() string {
	ms := time.Now().UnixMilli()
	return strconv.FormatInt(ms, 36) + "-" + Random(EventSuffix)
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
