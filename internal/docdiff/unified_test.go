package docdiff

import (
	"strings"
	"testing"

	"github.com/six-ddc/artx/internal/api"
)

func numbered(from, to int) string {
	var sb strings.Builder
	for i := from; i <= to; i++ {
		sb.WriteString("line ")
		sb.WriteByte(byte('0' + i/10))
		sb.WriteByte(byte('0' + i%10))
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestUnifiedIdentical(t *testing.T) {
	src := []byte(numbered(1, 5))
	hunks := Unified(src, src)
	if hunks == nil || len(hunks) != 0 {
		t.Fatalf("hunks = %#v, want non-nil empty", hunks)
	}
}

func TestUnifiedSingleChange(t *testing.T) {
	srcA := numbered(1, 10)
	srcB := strings.Replace(srcA, "line 05", "line 05 changed", 1)
	hunks := Unified([]byte(srcA), []byte(srcB))
	if len(hunks) != 1 {
		t.Fatalf("hunks = %d, want 1", len(hunks))
	}
	h := hunks[0]
	if h.FromStart != 2 || h.ToStart != 2 {
		t.Fatalf("hunk starts = %d/%d, want 2/2", h.FromStart, h.ToStart)
	}
	if h.FromCount != 7 || h.ToCount != 7 {
		t.Fatalf("hunk counts = %d/%d, want 7/7 (3 ctx + change + 3 ctx)", h.FromCount, h.ToCount)
	}
	var dels, adds int
	for _, l := range h.Lines {
		switch l.Op {
		case api.DiffLineDel:
			dels++
			if l.Text != "line 05" {
				t.Fatalf("del line = %q", l.Text)
			}
		case api.DiffLineAdd:
			adds++
			if l.Text != "line 05 changed" {
				t.Fatalf("add line = %q", l.Text)
			}
		}
	}
	if dels != 1 || adds != 1 {
		t.Fatalf("dels/adds = %d/%d", dels, adds)
	}
}

func TestUnifiedTwoDistantChanges(t *testing.T) {
	srcA := numbered(1, 30)
	srcB := strings.Replace(srcA, "line 03", "line 03x", 1)
	srcB = strings.Replace(srcB, "line 25", "line 25x", 1)
	hunks := Unified([]byte(srcA), []byte(srcB))
	if len(hunks) != 2 {
		t.Fatalf("hunks = %d, want 2 (changes 22 lines apart must not merge)", len(hunks))
	}
	if hunks[1].FromStart != 22 {
		t.Fatalf("second hunk FromStart = %d, want 22", hunks[1].FromStart)
	}
}

func TestUnifiedNearbyChangesMerge(t *testing.T) {
	srcA := numbered(1, 20)
	srcB := strings.Replace(srcA, "line 08", "line 08x", 1)
	srcB = strings.Replace(srcB, "line 12", "line 12x", 1)
	hunks := Unified([]byte(srcA), []byte(srcB))
	if len(hunks) != 1 {
		t.Fatalf("hunks = %d, want 1 (3-line gap merges)", len(hunks))
	}
}

func TestUnifiedPureAddToEmpty(t *testing.T) {
	hunks := Unified(nil, []byte("alpha\nbeta\n"))
	if len(hunks) != 1 {
		t.Fatalf("hunks = %d, want 1", len(hunks))
	}
	h := hunks[0]
	if h.FromCount != 0 || h.ToCount != 2 {
		t.Fatalf("counts = %d/%d, want 0/2", h.FromCount, h.ToCount)
	}
	for _, l := range h.Lines {
		if l.Op != api.DiffLineAdd {
			t.Fatalf("op = %q, want add", l.Op)
		}
	}
}
