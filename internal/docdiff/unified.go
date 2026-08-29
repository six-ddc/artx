package docdiff

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/six-ddc/artx/internal/api"
)

// hunkContext is the number of unchanged lines kept around each change,
// matching the unified-diff convention.
const hunkContext = 3

// lineRec is one source line in the flattened diff stream. aLine/bLine are
// the 1-based line numbers this record sits at in the old/new file; an add
// carries the *next unconsumed* old line (and symmetrically for del), which
// is exactly what a hunk header needs when it opens on that record.
type lineRec struct {
	op           string
	text         string
	aLine, bLine int
}

// Unified computes line-mode hunks over the raw source with hunkContext
// context lines. It deliberately does NOT shell out to `git diff`: sharing
// newDMP with BlockOps keeps the rendered and source views consistent, and a
// working-copy target needs no special casing.
func Unified(srcA, srcB []byte) []api.DiffHunk {
	dmp := newDMP()
	ca, cb, lineArray := dmp.DiffLinesToChars(string(srcA), string(srcB))
	diffs := dmp.DiffCharsToLines(dmp.DiffMain(ca, cb, false), lineArray)

	var recs []lineRec
	aLine, bLine := 1, 1
	for _, d := range diffs {
		for _, text := range splitLines(d.Text) {
			switch d.Type {
			case diffmatchpatch.DiffEqual:
				recs = append(recs, lineRec{op: api.DiffLineCtx, text: text, aLine: aLine, bLine: bLine})
				aLine++
				bLine++
			case diffmatchpatch.DiffDelete:
				recs = append(recs, lineRec{op: api.DiffLineDel, text: text, aLine: aLine, bLine: bLine})
				aLine++
			case diffmatchpatch.DiffInsert:
				recs = append(recs, lineRec{op: api.DiffLineAdd, text: text, aLine: aLine, bLine: bLine})
				bLine++
			}
		}
	}

	// Fold the stream into hunks: a run of context lines no longer than
	// 2*hunkContext between two changes is swallowed into one hunk.
	hunks := []api.DiffHunk{}
	n := len(recs)
	i := 0
	for i < n {
		if recs[i].op == api.DiffLineCtx {
			i++
			continue
		}
		start := max(i-hunkContext, 0)
		last := i
		j := i + 1
		for j < n {
			if recs[j].op != api.DiffLineCtx {
				last = j
				j++
				continue
			}
			k := j
			for k < n && recs[k].op == api.DiffLineCtx {
				k++
			}
			if k < n && k-j <= 2*hunkContext {
				j = k
				continue
			}
			break
		}
		end := min(last+hunkContext+1, n)
		h := api.DiffHunk{FromStart: recs[start].aLine, ToStart: recs[start].bLine}
		for _, rec := range recs[start:end] {
			h.Lines = append(h.Lines, api.DiffLine{Op: rec.op, Text: rec.text})
			if rec.op != api.DiffLineAdd {
				h.FromCount++
			}
			if rec.op != api.DiffLineDel {
				h.ToCount++
			}
		}
		hunks = append(hunks, h)
		i = end
	}
	return hunks
}

// splitLines splits a diff segment into lines. The segment's trailing "\n"
// (when present) terminates its last line rather than opening an empty one;
// a segment without one still counts as a line — the file's unterminated
// last line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
