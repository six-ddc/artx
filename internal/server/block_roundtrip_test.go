package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/render"
	"github.com/six-ddc/artx/internal/vault"
)

// The block editor's core invariant: for EVERY data-sourcepos range the
// renderer emits — nested lists, blockquotes inside blockquotes, fences
// containing fence markers, tables, CJK/emoji multibyte content — the range
// is a valid byte slice of the source, writing the slice back unchanged is
// a byte-identical no-op, and writing a replacement touches exactly that
// range while the document still renders.
//
// This is what makes "edit the source slice" safe where "convert HTML back
// to markdown" is not: the round-trip is exact by construction, and this
// test pins the construction.

// fence pieces are concatenated because Go raw strings cannot contain backticks.
var complexMD = "---\n" +
	"aid: mdprop1\n" +
	"title: 复杂文档\n" +
	"---\n" +
	"\n" +
	"# 顶层标题 🎯\n" +
	"\n" +
	"第一段，包含 `行内代码` 和 **加粗**，以及中文标点。\n" +
	"\n" +
	"## 嵌套列表\n" +
	"\n" +
	"- 一级项 A\n" +
	"  - 二级项 B，带 `code`\n" +
	"    - 三级项 C 🚀\n" +
	"- 一级项 D\n" +
	"\n" +
	"> 引用块第一行\n" +
	">\n" +
	"> > 嵌套引用，内含列表：\n" +
	"> > 1. 有序一\n" +
	"> > 2. 有序二\n" +
	"\n" +
	"````text\n" +
	"外层围栏，内部包含 ```三反引号``` 也不能截断\n" +
	"````\n" +
	"\n" +
	"```mermaid\n" +
	"graph TD; A-->B;\n" +
	"```\n" +
	"\n" +
	"| 列一 | 列二 |\n" +
	"|------|------|\n" +
	"| 中文 🀄 | `cell code` |\n" +
	"\n" +
	"Setext 标题\n" +
	"---------\n" +
	"\n" +
	"尾段落。\n"

var sourceposRe = regexp.MustCompile(`data-sourcepos="(\d+):(\d+)"`)

func TestHandleDocBlockRoundTripsEveryRenderedBlock(t *testing.T) {
	src := []byte(complexMD)
	res, err := render.New().Render(src)
	if err != nil {
		t.Fatal(err)
	}

	type rng struct{ start, end int }
	seen := map[rng]bool{}
	var ranges []rng
	for _, m := range sourceposRe.FindAllStringSubmatch(res.HTML, -1) {
		start, _ := strconv.Atoi(m[1])
		end, _ := strconv.Atoi(m[2])
		r := rng{start, end}
		if !seen[r] {
			seen[r] = true
			ranges = append(ranges, r)
		}
	}
	// The fixture must actually exercise the hard cases; a rendering change
	// that stops emitting sourcepos would silently gut this test otherwise.
	if len(ranges) < 10 {
		t.Fatalf("expected a rich block table, got %d ranges. html:\n%s", len(ranges), res.HTML)
	}

	dir := t.TempDir()
	mdPath := filepath.Join(dir, "index.md")
	fv := newFakeVault()
	art := &vault.Artifact{ID: "mdprop1", Slug: "prop", Type: api.DocTypeMD, Dir: dir, Path: mdPath}
	s, _ := newTestServerWithVault(t, fv)

	reset := func() {
		if err := os.WriteFile(mdPath, src, 0o644); err != nil {
			t.Fatal(err)
		}
		fv.put(art, src)
	}

	post := func(start, end int, original, content string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{
			"start": start, "end": end, "original": original, "content": content,
		})
		r := httptest.NewRequest(http.MethodPost, "/api/docs/mdprop1/block", bytes.NewReader(raw))
		r.SetPathValue("id", "mdprop1")
		w := httptest.NewRecorder()
		s.handleDocBlock(w, r)
		return w
	}

	for i, r := range ranges {
		// Range sanity: inside the file, never overlapping the frontmatter.
		if r.start < res.BodyOffset || r.end > len(src) || r.start > r.end {
			t.Fatalf("range %d (%d:%d) escapes body [%d, %d)", i, r.start, r.end, res.BodyOffset, len(src))
		}
		slice := string(src[r.start:r.end])

		// Identity write-back: byte-identical no-op.
		reset()
		if w := post(r.start, r.end, slice, slice); w.Code != http.StatusOK {
			t.Fatalf("identity write of range %d:%d failed: %d %s", r.start, r.end, w.Code, w.Body.String())
		}
		got, _ := os.ReadFile(mdPath)
		if !bytes.Equal(got, src) {
			t.Fatalf("identity write of %d:%d changed the file.\nslice: %q\ngot:\n%s", r.start, r.end, slice, got)
		}

		// Replacement: exactly this range changes, and the result renders.
		reset()
		marker := fmt.Sprintf("EDITED-%d", i)
		if w := post(r.start, r.end, slice, marker); w.Code != http.StatusOK {
			t.Fatalf("replacement write of range %d:%d failed: %d %s", r.start, r.end, w.Code, w.Body.String())
		}
		got, _ = os.ReadFile(mdPath)
		want := string(src[:r.start]) + marker + string(src[r.end:])
		if string(got) != want {
			t.Fatalf("replacement of %d:%d leaked outside the range.\nslice: %q\ngot:\n%s", r.start, r.end, slice, got)
		}
		if _, err := render.New().Render(got); err != nil {
			t.Fatalf("document no longer renders after editing %d:%d: %v", r.start, r.end, err)
		}
	}
	reset()
}
