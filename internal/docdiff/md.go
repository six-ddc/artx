package docdiff

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/mdsrc"
)

// modifiedThreshold is the minimum byte-level similarity for a paired
// delete/insert to count as a modification of the same block (reuses the 0.5
// threshold precedent from remap's bitap matching).
const modifiedThreshold = 0.5

// blockSpan is one top-level block's file-absolute range plus its raw source
// slice.
type blockSpan struct {
	start, end int
	text       string
}

// topBlocks extracts the Depth==0 block sequence. mdsrc offsets are
// file-absolute, so they line up with the data-sourcepos values in rendered
// HTML by construction. Zero-width blocks (a ThematicBreak has no Lines and
// aggregates to nothing) are dropped — they also render without a
// data-sourcepos, so both sides skip them consistently.
func topBlocks(src []byte) []blockSpan {
	doc, err := mdsrc.Parse(src)
	if err != nil {
		return nil
	}
	var out []blockSpan
	for _, b := range doc.Blocks {
		if b.Depth != 0 || b.End <= b.Start {
			continue
		}
		out = append(out, blockSpan{start: b.Start, end: b.End, text: string(src[b.Start:b.End])})
	}
	return out
}

// encodeBlocks maps each distinct (whitespace-trimmed) block text to one rune
// so DiffMain can align whole blocks the way DiffLinesToChars aligns lines.
// The surrogate range is skipped: those code points cannot round-trip through
// a Go string and would collapse distinct blocks into U+FFFD.
func encodeBlocks(blocks []blockSpan, table map[string]rune, next *rune) string {
	var sb strings.Builder
	for _, b := range blocks {
		key := strings.TrimSpace(b.text)
		r, ok := table[key]
		if !ok {
			r = *next
			*next++
			if *next == 0xD800 {
				*next = 0xE000
			}
			table[key] = r
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// BlockOps aligns the two versions' top-level blocks and classifies each as
// unchanged / modified / added / removed.
//
// Algorithm: hash each block to a rune, DiffMain over the two rune strings,
// then greedily pair adjacent delete/insert runs in order — a pair whose
// byte-level similarity reaches modifiedThreshold becomes one modified op
// (with word-level added/removed source segments), anything unpaired falls
// out as removed/added. Emitting ops while walking the diff runs yields
// new-document order with removed blocks in front of their old successor.
func BlockOps(srcA, srcB []byte) ([]api.DiffBlock, api.DiffStats) {
	a, b := topBlocks(srcA), topBlocks(srcB)
	table := map[string]rune{}
	next := rune(1)
	ea := encodeBlocks(a, table, &next)
	eb := encodeBlocks(b, table, &next)

	dmp := newDMP()
	diffs := dmp.DiffMain(ea, eb, false)

	var out []api.DiffBlock
	var stats api.DiffStats
	ai, bi := 0, 0
	var pendDel, pendIns []int // block indexes into a / b awaiting pairing

	emitRemoved := func(i int) {
		out = append(out, api.DiffBlock{Op: api.DiffRemoved, From: []int{a[i].start, a[i].end}})
		stats.Removed++
	}
	emitAdded := func(i int) {
		out = append(out, api.DiffBlock{Op: api.DiffAdded, To: []int{b[i].start, b[i].end}})
		stats.Added++
	}
	// flush pairs a pending delete-run against a pending insert-run with two
	// pointers: a similar-enough head pair becomes one modified op. On a
	// mismatch, a one-step lookahead to each side decides which pointer
	// advances alone — whichever skip preserves the better future pair wins —
	// so a deletion or insertion sitting right next to a modification doesn't
	// steal its partner (and removed still lands before added at a junction).
	flush := func() {
		di, ii := 0, 0
		for di < len(pendDel) || ii < len(pendIns) {
			switch {
			case di >= len(pendDel):
				emitAdded(pendIns[ii])
				ii++
			case ii >= len(pendIns):
				emitRemoved(pendDel[di])
				di++
			default:
				oa, ob := a[pendDel[di]], b[pendIns[ii]]
				added, removed, sim := wordDiff(dmp, oa.text, ob.text)
				if sim >= modifiedThreshold {
					out = append(out, api.DiffBlock{
						Op:           api.DiffModified,
						From:         []int{oa.start, oa.end},
						To:           []int{ob.start, ob.end},
						AddedTexts:   added,
						RemovedTexts: removed,
					})
					stats.Modified++
					di++
					ii++
					continue
				}
				skipIns, skipDel := 0.0, 0.0
				if ii+1 < len(pendIns) {
					_, _, skipIns = wordDiff(dmp, oa.text, b[pendIns[ii+1]].text)
				}
				if di+1 < len(pendDel) {
					_, _, skipDel = wordDiff(dmp, a[pendDel[di+1]].text, ob.text)
				}
				if skipIns >= modifiedThreshold && skipIns > skipDel {
					emitAdded(pendIns[ii])
					ii++
					continue
				}
				emitRemoved(pendDel[di])
				di++
			}
		}
		pendDel, pendIns = nil, nil
	}

	for _, d := range diffs {
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			flush()
			for range d.Text {
				out = append(out, api.DiffBlock{Op: api.DiffUnchanged, To: []int{b[bi].start, b[bi].end}})
				ai++
				bi++
			}
		case diffmatchpatch.DiffDelete:
			for range d.Text {
				pendDel = append(pendDel, ai)
				ai++
			}
		case diffmatchpatch.DiffInsert:
			for range d.Text {
				pendIns = append(pendIns, bi)
				bi++
			}
		}
	}
	flush()
	return out, stats
}

// wordDiff diffs one candidate block pair byte-level and reports their
// similarity (shared bytes over total, 1 = identical) alongside the changed
// segments. The segments are raw source text — the frontend locates them in
// the rendered block itself and silently degrades when a match fails, so no
// ins/del HTML is synthesized here. Segments are only worth extracting for a
// pair the caller will accept, so a below-threshold pair returns nil ones.
func wordDiff(dmp *diffmatchpatch.DiffMatchPatch, oldText, newText string) (added, removed []string, sim float64) {
	diffs := dmp.DiffMain(oldText, newText, false)

	equal := 0
	for _, d := range diffs {
		if d.Type == diffmatchpatch.DiffEqual {
			equal += len(d.Text)
		}
	}
	sim = 1
	if total := len(oldText) + len(newText); total > 0 {
		sim = float64(2*equal) / float64(total)
	}
	if sim < modifiedThreshold {
		return nil, nil, sim
	}

	diffs = dmp.DiffCleanupSemantic(diffs)
	for _, d := range diffs {
		if strings.TrimSpace(d.Text) == "" {
			continue
		}
		switch d.Type {
		case diffmatchpatch.DiffInsert:
			added = append(added, d.Text)
		case diffmatchpatch.DiffDelete:
			removed = append(removed, d.Text)
		}
	}
	return added, removed, sim
}
