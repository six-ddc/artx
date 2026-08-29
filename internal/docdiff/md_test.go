package docdiff

import (
	"strings"
	"testing"

	"github.com/six-ddc/artx/internal/api"
)

// ops flattens the op sequence for compact assertions.
func ops(blocks []api.DiffBlock) []string {
	var out []string
	for _, b := range blocks {
		out = append(out, b.Op)
	}
	return out
}

func assertOps(t *testing.T, blocks []api.DiffBlock, want ...string) {
	t.Helper()
	got := ops(blocks)
	if len(got) != len(want) {
		t.Fatalf("ops = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ops = %v, want %v", got, want)
		}
	}
}

func TestBlockOpsIdentical(t *testing.T) {
	src := []byte("# Title\n\nParagraph one.\n\nParagraph two.\n")
	blocks, stats := BlockOps(src, src)
	assertOps(t, blocks, api.DiffUnchanged, api.DiffUnchanged, api.DiffUnchanged)
	if stats != (api.DiffStats{}) {
		t.Fatalf("stats = %+v, want zero", stats)
	}
}

func TestBlockOpsFrontmatterOnlyChange(t *testing.T) {
	srcA := []byte("---\ntitle: old\n---\n\n# Title\n\nBody text.\n")
	srcB := []byte("---\ntitle: brand new title\n---\n\n# Title\n\nBody text.\n")
	blocks, stats := BlockOps(srcA, srcB)
	// The body is untouched; the frontmatter flag is the handler's business.
	assertOps(t, blocks, api.DiffUnchanged, api.DiffUnchanged)
	if stats != (api.DiffStats{}) {
		t.Fatalf("stats = %+v, want zero", stats)
	}
	// Offsets must stay file-absolute: the new frontmatter is longer, so the
	// heading's To range must point into srcB, not srcA. (A Heading's span
	// excludes its "# " prefix, mirroring the rendered data-sourcepos.)
	if want := strings.Index(string(srcB), "Title\n\nBody"); blocks[0].To[0] != want {
		t.Fatalf("heading To = %v, want start %d", blocks[0].To, want)
	}
}

func TestBlockOpsPureAdd(t *testing.T) {
	srcA := []byte("First.\n\nLast.\n")
	srcB := []byte("First.\n\nMiddle inserted.\n\nLast.\n")
	blocks, stats := BlockOps(srcA, srcB)
	assertOps(t, blocks, api.DiffUnchanged, api.DiffAdded, api.DiffUnchanged)
	if stats.Added != 1 || stats.Removed != 0 || stats.Modified != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	add := blocks[1]
	if got := string(srcB[add.To[0]:add.To[1]]); got != "Middle inserted." {
		t.Fatalf("added To slice = %q", got)
	}
	if add.From != nil {
		t.Fatalf("added block must carry no From, got %v", add.From)
	}
}

func TestBlockOpsPureRemoveOrdering(t *testing.T) {
	srcA := []byte("First.\n\nDoomed paragraph.\n\nLast.\n")
	srcB := []byte("First.\n\nLast.\n")
	blocks, stats := BlockOps(srcA, srcB)
	// The removed block sits where it used to: before its old successor.
	assertOps(t, blocks, api.DiffUnchanged, api.DiffRemoved, api.DiffUnchanged)
	if stats.Removed != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	rm := blocks[1]
	if got := string(srcA[rm.From[0]:rm.From[1]]); got != "Doomed paragraph." {
		t.Fatalf("removed From slice = %q", got)
	}
	if rm.To != nil {
		t.Fatalf("removed block must carry no To, got %v", rm.To)
	}
}

func TestBlockOpsModified(t *testing.T) {
	srcA := []byte("Intro.\n\nThe quick brown fox jumps over the lazy dog.\n\nOutro.\n")
	srcB := []byte("Intro.\n\nThe quick red fox jumps over the lazy dog.\n\nOutro.\n")
	blocks, stats := BlockOps(srcA, srcB)
	assertOps(t, blocks, api.DiffUnchanged, api.DiffModified, api.DiffUnchanged)
	if stats.Modified != 1 || stats.Added != 0 || stats.Removed != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	mod := blocks[1]
	if len(mod.From) != 2 || len(mod.To) != 2 {
		t.Fatalf("modified needs both ranges: %+v", mod)
	}
	if strings.Join(mod.AddedTexts, "|") != "red" {
		t.Fatalf("added_texts = %v", mod.AddedTexts)
	}
	if strings.Join(mod.RemovedTexts, "|") != "brown" {
		t.Fatalf("removed_texts = %v", mod.RemovedTexts)
	}
}

func TestBlockOpsDissimilarReplacement(t *testing.T) {
	srcA := []byte("Stable.\n\nAlpha beta gamma delta epsilon.\n")
	srcB := []byte("Stable.\n\nCompletely unrelated replacement text with nothing shared whatsoever, entirely rewritten from scratch.\n")
	blocks, stats := BlockOps(srcA, srcB)
	// Below the 0.5 similarity bar the pair falls apart into removed+added.
	assertOps(t, blocks, api.DiffUnchanged, api.DiffRemoved, api.DiffAdded)
	if stats.Removed != 1 || stats.Added != 1 || stats.Modified != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestBlockOpsRemoveAdjacentToModify(t *testing.T) {
	srcA := []byte("# Title\n\nDoomed paragraph.\n\nThe quick brown fox jumps.\n")
	srcB := []byte("# Title\n\nThe quick red fox jumps.\n\nFresh paragraph.\n")
	blocks, stats := BlockOps(srcA, srcB)
	// The delete-run holds [Doomed, quick-brown] and the insert-run
	// [quick-red, Fresh]: naive positional pairing would mismatch both; the
	// lookahead must recover removed + modified + added.
	assertOps(t, blocks, api.DiffUnchanged, api.DiffRemoved, api.DiffModified, api.DiffAdded)
	if stats.Removed != 1 || stats.Modified != 1 || stats.Added != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestBlockOpsInsertAdjacentToModify(t *testing.T) {
	srcA := []byte("# Title\n\nThe quick brown fox jumps.\n")
	srcB := []byte("# Title\n\nBrand new paragraph.\n\nThe quick red fox jumps.\n")
	blocks, stats := BlockOps(srcA, srcB)
	assertOps(t, blocks, api.DiffUnchanged, api.DiffAdded, api.DiffModified)
	if stats.Added != 1 || stats.Modified != 1 || stats.Removed != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestBlockOpsMove(t *testing.T) {
	srcA := []byte("Alpha paragraph.\n\nBeta paragraph.\n\nGamma paragraph.\n")
	srcB := []byte("Beta paragraph.\n\nGamma paragraph.\n\nAlpha paragraph.\n")
	blocks, _ := BlockOps(srcA, srcB)
	// A move is reported as removed at the old position + added at the new
	// one; the equal middle run must stay unchanged.
	assertOps(t, blocks, api.DiffRemoved, api.DiffUnchanged, api.DiffUnchanged, api.DiffAdded)
	if got := string(srcB[blocks[3].To[0]:blocks[3].To[1]]); got != "Alpha paragraph." {
		t.Fatalf("moved block To slice = %q", got)
	}
}

func TestBlockOpsWhitespaceOnlyChangeIsUnchanged(t *testing.T) {
	srcA := []byte("One.\n\nTwo.\n")
	srcB := []byte("One.\n\nTwo.\n\n")
	blocks, stats := BlockOps(srcA, srcB)
	assertOps(t, blocks, api.DiffUnchanged, api.DiffUnchanged)
	if stats != (api.DiffStats{}) {
		t.Fatalf("stats = %+v, want zero", stats)
	}
}

func TestBlockOpsEmptyOldSource(t *testing.T) {
	blocks, stats := BlockOps(nil, []byte("Only paragraph.\n"))
	assertOps(t, blocks, api.DiffAdded)
	if stats.Added != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}
