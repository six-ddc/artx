package docdiff

import (
	"strings"
	"testing"

	"github.com/six-ddc/artx/internal/render"
)

func TestTopLevelBySourceposBlockquote(t *testing.T) {
	// The blockquote and the single paragraph inside it span the same source
	// range, so the same "start:end" appears at two depths. The index must
	// hand back the top-level blockquote, not the inner paragraph.
	src := []byte("> quoted line\n\nplain paragraph\n")
	res, err := render.New().Render(src)
	if err != nil {
		t.Fatal(err)
	}
	idx := TopLevelBySourcepos(res.HTML)
	if len(idx) != 2 {
		t.Fatalf("index size = %d, want 2 top-level blocks: %v", len(idx), idx)
	}
	var quote string
	for _, h := range idx {
		if strings.Contains(h, "quoted line") {
			quote = h
		}
	}
	if !strings.HasPrefix(quote, "<blockquote") {
		t.Fatalf("blockquote entry = %q, want the outer element", quote)
	}
}
