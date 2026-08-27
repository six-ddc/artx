// Package anchor defines the anchor data structure and resolves an anchor
// to its current position in a document.
//
// Owned by: W-anchor. The Anchor struct itself is a **frozen contract**
// (serialized into YAML by eventlog, mapped into JSON by api); implementers
// may only fill in function bodies, never rename fields or change tags.
package anchor

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/mdsrc"
)

// ContextChars is the target length (in runes, not bytes) of the prefix/suffix.
const ContextChars = 32

// ContextLines is the number of lines taken before and after in api.ThreadAnchor.Context.
const ContextLines = 2

// ErrNoMatch means the anchor could not be located in the source text.
var ErrNoMatch = errors.New("anchor: no match")

// bitap parameters. Kept identical to remap.DefaultOptions: anchor cannot
// import remap (it would create a cycle), so these are defined independently
// here — the two copies must be kept in sync by hand.
const (
	matchThreshold = 0.5
	matchDistance  = 1000
	// matchMaxBits is the upper bound on the bitap pattern length.
	// diffmatchpatch.MatchBitap builds its matchmask as 1<<(len(pattern)-1),
	// which silently misbehaves once the pattern exceeds the int width.
	// Upstream only truncates in PatchApply, not in MatchMain — so the
	// caller must truncate itself.
	matchMaxBits = 32
)

// Anchor is the **persisted form** of an anchor, serialized verbatim into
// the event log's anchor field.
//
// W3C-style dual selector:
//   - TextQuoteSelector: Exact + Prefix/Suffix — the primary selector,
//     resilient to content drift.
//   - TextPositionSelector: Start/End byte offsets — a fast-path hint that
//     may go stale.
//
// Start/End are always **absolute byte offsets into the whole file**
// (frontmatter included), as a half-open range [Start, End).
type Anchor struct {
	Kind   string `yaml:"kind" json:"kind"` // api.AnchorText | api.AnchorElement
	Exact  string `yaml:"exact,omitempty" json:"exact,omitempty"`
	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`
	Suffix string `yaml:"suffix,omitempty" json:"suffix,omitempty"`
	Start  int    `yaml:"start,omitempty" json:"start,omitempty"`
	End    int    `yaml:"end,omitempty" json:"end,omitempty"`
	Rev    string `yaml:"rev,omitempty" json:"rev,omitempty"`
	AID    string `yaml:"aid,omitempty" json:"aid,omitempty"`       // element anchor
	Approx bool   `yaml:"approx,omitempty" json:"approx,omitempty"` // block-level fallback, not an exact hit
}

// Match is the result of a single locate attempt.
type Match struct {
	Start  int     // absolute byte offset into the file
	End    int     // absolute byte offset into the file
	Score  float64 // 1.0 = exact hit; 0 < s < 1 = fuzzy hit; 0 = no hit
	Approx bool    // true = degraded to the block-level fallback
}

// FromSelection converts a browser-reported markdown selection into an Anchor.
//
// Algorithm (Go-side implementation of the "md selection → anchor" section
// in blueprint.md):
//  1. Locate the block covering sel.BlockStart/BlockEnd in doc, and take that
//     block's concatenated segment text blockText (segments already have
//     block-level prefixes such as "> " and list indentation stripped).
//  2. Search blockText for an exact match of sel.Exact; adopt it if the
//     match is unique.
//  3. On no match or multiple matches, use the tail of sel.Before as a
//     positional hint and fall back to bitap fuzzy matching
//     (diffmatchpatch.MatchMain, threshold MatchThreshold).
//  4. If that still fails, return the whole block's range with Approx=true.
//  5. Map the hit position back through the block's segment mapping to an
//     absolute file offset, then fill Exact/Prefix/Suffix from the source text.
//
// The returned Anchor.Kind is always api.AnchorText; Rev is filled by the caller.
//
// There is an extra step wedged between steps 2 and 3: inline markdown
// markup is stripped from the block source before the second exact-match
// attempt. What the frontend reports as "exact" is the **rendered** text
// (e.g. `**bold**` has already become `bold`), so searching the raw source
// for it verbatim is bound to miss — while bitap on a short string that
// still contains markup tends to pick the wrong spot. Matching against the
// markup-stripped text is both deterministic and handles the vast majority
// of rendered-vs-source differences in one pass.
func FromSelection(doc *mdsrc.Document, sel api.SelectionInput) (Anchor, error) {
	if doc == nil {
		return Anchor{}, ErrNoMatch
	}
	src := doc.Source

	blockStart, blockEnd := clampRange(len(src), sel.BlockStart, sel.BlockEnd)
	block := doc.BlockAt(sel.BlockStart, sel.BlockEnd)
	if block == nil {
		block = doc.BlockCovering(sel.BlockStart)
	}

	fallback := func() (Anchor, error) {
		s, e := blockStart, blockEnd
		if block != nil {
			s, e = clampRange(len(src), block.Start, block.End)
		}
		a := newTextAnchor(src, s, e)
		a.Approx = true
		return a, nil
	}

	if block == nil || strings.TrimSpace(sel.Exact) == "" {
		return fallback()
	}

	bm := doc.BlockMap(block)
	if bm.Text == "" {
		return fallback()
	}

	// Level 1: exact match directly in the block source text (hits when the
	// selection doesn't include any markup that rendering would have stripped).
	if i := exactSearch(bm.Text, sel.Exact, sel.Before); i >= 0 {
		s, e := bm.Range(i, i+len(sel.Exact))
		return newTextAnchor(src, s, e), nil
	}

	// Level 2: exact match after stripping inline markup, mapping the index
	// back into the block source text.
	plain, idx := stripInline(bm.Text)
	if i := exactSearch(plain, sel.Exact, sel.Before); i >= 0 {
		s, e := bm.Range(mapStart(idx, i), mapEnd(idx, i+len(sel.Exact)))
		return newTextAnchor(src, s, e), nil
	}

	// Level 3: bitap fuzzy matching. Prefer the markup-stripped text — it's
	// closest to the rendered form.
	dmp := newDMP()
	if i, ok := fuzzySearch(dmp, plain, sel.Exact, sel.Before); ok {
		j := clampInt(i+len(sel.Exact), i, len(plain))
		s, e := bm.Range(mapStart(idx, i), mapEnd(idx, j))
		return newTextAnchor(src, s, e), nil
	}
	if i, ok := fuzzySearch(dmp, bm.Text, sel.Exact, sel.Before); ok {
		j := clampInt(i+len(sel.Exact), i, len(bm.Text))
		s, e := bm.Range(i, j)
		return newTextAnchor(src, s, e), nil
	}

	// Level 4: fall back to the whole block.
	return fallback()
}

// FromElement converts an html-artifact element selection into an Anchor
// (Kind = api.AnchorElement). Start/End are meaningless for element anchors
// and are always 0.
func FromElement(el api.ElementInput) (Anchor, error) {
	if strings.TrimSpace(el.AID) == "" {
		return Anchor{}, ErrNoMatch
	}
	return Anchor{
		Kind:  api.AnchorElement,
		AID:   el.AID,
		Exact: el.Quote,
	}, nil
}

// Locate re-locates an anchor in the given source text. Used both to
// refresh anchors before rendering and to validate them during remap.
//
// Order:
//  1. If src[a.Start:a.End] == a.Exact, that's a direct hit (Score=1).
//  2. Otherwise search the whole text for a.Prefix+a.Exact+a.Suffix,
//     falling back to searching for a.Exact alone.
//  3. Otherwise fall back to bitap fuzzy search, expecting a.Start as the
//     target position.
//  4. If all of the above fail, return ErrNoMatch.
//
// Levels 1 and 2 both score 1.0 — both are byte-identical hits, differing
// only in position; callers distinguish them by checking whether
// Match.Start equals a.Start. Only level 3 ever produces a score below 1.
func Locate(src []byte, a Anchor) (Match, error) {
	if a.Exact == "" {
		return Match{}, ErrNoMatch
	}
	text := string(src)

	// 1. Direct hit at the stored offset.
	if a.Start >= 0 && a.End <= len(src) && a.Start <= a.End &&
		string(src[a.Start:a.End]) == a.Exact {
		return Match{Start: a.Start, End: a.End, Score: 1, Approx: a.Approx}, nil
	}

	// 2. Exact search for prefix+exact+suffix across the whole text, falling
	// back to searching for exact alone.
	if a.Prefix != "" || a.Suffix != "" {
		pat := a.Prefix + a.Exact + a.Suffix
		if i := nearestIndex(text, pat, a.Start); i >= 0 {
			s := i + len(a.Prefix)
			return Match{Start: s, End: s + len(a.Exact), Score: 1, Approx: a.Approx}, nil
		}
	}
	if i := nearestIndex(text, a.Exact, a.Start); i >= 0 {
		return Match{Start: i, End: i + len(a.Exact), Score: 1, Approx: a.Approx}, nil
	}

	// 3. bitap.
	dmp := newDMP()
	pat := truncatePattern(a.Exact, matchMaxBits)
	if pat != "" {
		loc := clampInt(a.Start, 0, len(text))
		if i := dmp.MatchMain(text, pat, loc); i >= 0 {
			end := clampInt(i+len(a.Exact), i, len(text))
			score := similarity(dmp, text[i:end], a.Exact)
			return Match{Start: i, End: end, Score: score, Approx: a.Approx}, nil
		}
	}

	return Match{}, ErrNoMatch
}

// Enrich fills in the derived Line and Context fields of api.ThreadAnchor
// from the source text. threads is modified in place. When doc is nil
// (e.g. for an html document), the markdown-only fields are simply skipped.
func Enrich(src []byte, doc *mdsrc.Document, threads []api.Thread) {
	if doc == nil {
		return
	}
	for i := range threads {
		a := &threads[i].Anchor
		if a.Kind == api.AnchorElement {
			continue
		}
		s, e := clampRange(len(doc.Source), a.Start, a.End)
		a.Line = doc.LineOf(s)
		a.Context = doc.Context(s, e, ContextLines)
	}
	_ = src
}

// ToAPI converts a persisted anchor into its presentation form. Line/Context
// are left empty, to be filled in later by Enrich.
func ToAPI(a Anchor) api.ThreadAnchor {
	return api.ThreadAnchor{
		Kind:   a.Kind,
		Exact:  a.Exact,
		Prefix: a.Prefix,
		Suffix: a.Suffix,
		Start:  a.Start,
		End:    a.End,
		AID:    a.AID,
		Rev:    a.Rev,
		Approx: a.Approx,
	}
}

// Quote extracts the exact text for [start,end) from src, along with up to
// ContextChars runes of surrounding text as prefix/suffix.
func Quote(src []byte, start, end int) (exact, prefix, suffix string) {
	s, e := clampRange(len(src), start, end)
	exact = string(src[s:e])
	prefix = lastRunes(string(src[:s]), ContextChars)
	suffix = firstRunes(string(src[e:]), ContextChars)
	return exact, prefix, suffix
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func newDMP() *diffmatchpatch.DiffMatchPatch {
	dmp := diffmatchpatch.New()
	dmp.MatchThreshold = matchThreshold
	dmp.MatchDistance = matchDistance
	return dmp
}

func newTextAnchor(src []byte, start, end int) Anchor {
	exact, prefix, suffix := Quote(src, start, end)
	s, e := clampRange(len(src), start, end)
	return Anchor{
		Kind:   api.AnchorText,
		Exact:  exact,
		Prefix: prefix,
		Suffix: suffix,
		Start:  s,
		End:    e,
	}
}

// exactSearch finds needle in text exactly; when it occurs more than once,
// it returns the occurrence nearest to the position hinted by before.
func exactSearch(text, needle, before string) int {
	if needle == "" {
		return -1
	}
	return nearestIndex(text, needle, expectedPos(text, before))
}

// fuzzySearch runs bitap using the before hint as the expected position.
func fuzzySearch(dmp *diffmatchpatch.DiffMatchPatch, text, needle, before string) (int, bool) {
	pat := truncatePattern(needle, matchMaxBits)
	if pat == "" || text == "" {
		return 0, false
	}
	i := dmp.MatchMain(text, pat, clampInt(expectedPos(text, before), 0, len(text)))
	if i < 0 {
		return 0, false
	}
	return i, true
}

// nearestIndex returns the occurrence of needle in text closest to want,
// or -1 if needle does not occur.
func nearestIndex(text, needle string, want int) int {
	if needle == "" {
		return -1
	}
	best, from := -1, 0
	for {
		i := strings.Index(text[from:], needle)
		if i < 0 {
			break
		}
		i += from
		if best < 0 || abs(i-want) < abs(best-want) {
			best = i
		}
		from = i + 1
		if from >= len(text) {
			break
		}
	}
	return best
}

// expectedPos estimates the starting position of a selection from the
// rendered text that preceded it within the block. It's most accurate when
// the tail of before can be found in text; otherwise it falls back to the
// assumption that rendered-text length roughly tracks source-text length.
func expectedPos(text, before string) int {
	if before == "" {
		return 0
	}
	tail := lastRunes(before, ContextChars)
	for utf8.RuneCountInString(tail) >= 3 {
		if i := strings.LastIndex(text, tail); i >= 0 {
			return i + len(tail)
		}
		_, n := utf8.DecodeRuneInString(tail)
		tail = tail[n:]
	}
	return clampInt(len(before), 0, len(text))
}

// similarity converts a Levenshtein distance into a 0..1 similarity score,
// used as the Score for fuzzy hits.
func similarity(dmp *diffmatchpatch.DiffMatchPatch, a, b string) float64 {
	if a == b {
		return 1
	}
	if a == "" || b == "" {
		return 0.01
	}
	lev := dmp.DiffLevenshtein(dmp.DiffMain(a, b, false))
	span := utf8.RuneCountInString(a)
	if n := utf8.RuneCountInString(b); n > span {
		span = n
	}
	s := 1 - float64(lev)/float64(span)
	return clampFloat(s, 0.01, 0.99)
}

// truncatePattern cuts s to at most max bytes on a rune boundary, to stay
// under bitap's bit-width limit.
func truncatePattern(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// stripInline strips inline markdown markup from a block's source text,
// returning the stripped text and a mapping from
// "stripped-text byte index" to "block-source byte index" (length
// len(plain)+1).
//
// `_` is deliberately left untouched: snake_case identifiers are far more
// common than `_emphasis_` in technical docs, and stripping it would turn
// do_something into dosomething — creating a mismatch rather than fixing one.
func stripInline(s string) (string, []int) {
	var b strings.Builder
	idx := make([]int, 0, len(s)+1)
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]):
			// Escaped character: keep the escaped character itself.
			idx = append(idx, i+1)
			b.WriteByte(s[i+1])
			i += 2
			continue
		case c == '*' || c == '~' || c == '`':
			for i < len(s) && s[i] == c {
				i++
			}
			continue
		case c == '!' && i+1 < len(s) && s[i+1] == '[':
			i++ // drop the !, then handle the [ like a plain link
			continue
		case c == '[':
			i++
			continue
		case c == ']' && i+1 < len(s) && (s[i+1] == '(' || s[i+1] == '['):
			openCh, closeCh := s[i+1], byte(')')
			if openCh == '[' {
				closeCh = ']'
			}
			j, depth := i+2, 1
			for j < len(s) && depth > 0 {
				switch s[j] {
				case openCh:
					depth++
				case closeCh:
					depth--
				}
				j++
			}
			i = j
			continue
		}
		idx = append(idx, i)
		b.WriteByte(c)
		i++
	}
	idx = append(idx, len(s))
	return b.String(), idx
}

// mapStart maps a start index in the stripped text back to the block source text.
func mapStart(idx []int, i int) int {
	if len(idx) == 0 {
		return 0
	}
	return idx[clampInt(i, 0, len(idx)-1)]
}

// mapEnd maps an end index in the stripped text back to the block source text.
//
// It uses "index of the last kept byte, plus 1" rather than idx[j] directly:
// the latter would also swallow any markup immediately following that was
// stripped out (e.g. the trailing "**" of `bold**`) into the anchor.
func mapEnd(idx []int, j int) int {
	if len(idx) == 0 {
		return 0
	}
	j = clampInt(j, 0, len(idx)-1)
	if j == 0 {
		return idx[0]
	}
	return idx[j-1] + 1
}

func isASCIIPunct(c byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}

func firstRunes(s string, n int) string {
	i, cnt := 0, 0
	for i < len(s) && cnt < n {
		_, w := utf8.DecodeRuneInString(s[i:])
		i += w
		cnt++
	}
	return s[:i]
}

func lastRunes(s string, n int) string {
	i, cnt := len(s), 0
	for i > 0 && cnt < n {
		_, w := utf8.DecodeLastRuneInString(s[:i])
		i -= w
		cnt++
	}
	return s[i:]
}

func clampRange(n, start, end int) (int, int) {
	s := clampInt(start, 0, n)
	e := clampInt(end, s, n)
	return s, e
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
