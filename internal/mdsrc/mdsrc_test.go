package mdsrc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuin/goldmark/parser"
)

const sample = `---
aid: a7f3k2
title: 支付重构
---

# 标题一

第一段文字，包含 **粗体** 内容。
第二行继续。

> 引用块第一行
> 引用块第二行

- 列表项 A
- 列表项 B

` + "```mermaid\ngraph TD; A-->B;\n```" + `

最后一段。
`

func TestSplitFrontmatter(t *testing.T) {
	fm, off := SplitFrontmatter([]byte(sample))
	if !bytes.Contains(fm, []byte("aid: a7f3k2")) {
		t.Fatalf("frontmatter was not captured: %q", fm)
	}
	if got := sample[off : off+1]; got != "\n" && !strings.HasPrefix(sample[off:], "\n# ") {
		t.Fatalf("bodyOffset=%d points at %q", off, sample[off:off+10])
	}
	// Must not be misdetected when there is no frontmatter.
	if fm, off := SplitFrontmatter([]byte("# 标题\n")); fm != nil || off != 0 {
		t.Fatalf("misdetected frontmatter in a document without one: %q %d", fm, off)
	}
	// An unclosed --- must not swallow the body.
	if fm, off := SplitFrontmatter([]byte("---\naid: x\n# 没有闭合\n")); fm != nil || off != 0 {
		t.Fatalf("unclosed frontmatter should be treated as absent: %q %d", fm, off)
	}
}

// Block offsets must be **file-absolute offsets**: slicing the raw source
// must yield the block's content verbatim.
func TestBlockOffsetsAreAbsolute(t *testing.T) {
	src := []byte(sample)
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	var heading *Block
	for i := range doc.Blocks {
		if doc.Blocks[i].Kind == "Heading" && doc.Blocks[i].Level == 1 {
			heading = &doc.Blocks[i]
			break
		}
	}
	if heading == nil {
		t.Fatal("h1 not found")
	}
	if got := string(src[heading.Start:heading.End]); got != "标题一" {
		t.Fatalf("h1 offset is misaligned: %q", got)
	}
	if heading.Start <= doc.BodyOffset {
		t.Fatalf("offset was not shifted by BodyOffset: start=%d bodyOffset=%d", heading.Start, doc.BodyOffset)
	}
}

// Container blocks (Blockquote/List) have empty Lines() and must have their
// span aggregated from their descendants.
func TestContainerBlocksGetSpan(t *testing.T) {
	doc, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"Blockquote", "List"} {
		var found bool
		for _, b := range doc.Blocks {
			if b.Kind == kind {
				found = true
				if b.Start == 0 || b.End <= b.Start {
					t.Fatalf("%s did not get an aggregated span: %+v", kind, b)
				}
			}
		}
		if !found {
			t.Fatalf("no %s block found", kind)
		}
	}
}

// BlockMap must strip the "> " prefix, and its indices must map back to
// file-absolute offsets.
func TestBlockMapStripsMarkers(t *testing.T) {
	src := []byte(sample)
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	var quote *Block
	for i := range doc.Blocks {
		if doc.Blocks[i].Kind == "Paragraph" && strings.Contains(string(src[doc.Blocks[i].Start:doc.Blocks[i].End]), "引用块") {
			quote = &doc.Blocks[i]
			break
		}
	}
	if quote == nil {
		t.Fatal("paragraph inside blockquote not found")
	}
	m := doc.BlockMap(quote)
	if strings.Contains(m.Text, ">") {
		t.Fatalf("BlockMap did not strip the %q prefix: %q", "> ", m.Text)
	}
	i := strings.Index(m.Text, "引用块第二行")
	if i < 0 {
		t.Fatalf("block text is missing content: %q", m.Text)
	}
	start, end := m.Range(i, i+len("引用块第二行"))
	if got := string(src[start:end]); got != "引用块第二行" {
		t.Fatalf("mapping back to file offset is misaligned: %q (start=%d end=%d)", got, start, end)
	}
}

// The rendered data-sourcepos must match the block table, including for
// code blocks.
func TestRenderSourcePos(t *testing.T) {
	src := []byte(sample)
	_, bodyOffset := SplitFrontmatter(src)
	var buf bytes.Buffer
	md := NewMarkdown()
	if err := md.Convert(src[bodyOffset:], &buf, parser.WithContext(NewContext(bodyOffset))); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"<h1 data-sourcepos=", "<p data-sourcepos=", "<ul data-sourcepos=",
		"<blockquote data-sourcepos=", "<pre data-sourcepos=", `class="language-mermaid"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output is missing %q:\n%s", want, out)
		}
	}

	// The h1's data-sourcepos must slice out the heading text from the source.
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range doc.Blocks {
		if b.Kind == "Heading" && b.Level == 1 {
			if !strings.Contains(out, sourcePosAttr(b.Start, b.End)) {
				t.Fatalf("block table and rendered sourcepos disagree: block table %d:%d\n%s", b.Start, b.End, out)
			}
		}
	}
}

func sourcePosAttr(s, e int) string {
	var b strings.Builder
	b.WriteString(`data-sourcepos="`)
	b.WriteString(itoa(s))
	b.WriteByte(':')
	b.WriteString(itoa(e))
	b.WriteByte('"')
	return b.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	return string(d)
}

func TestLineAndContext(t *testing.T) {
	src := []byte(sample)
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	off := bytes.Index(src, []byte("第二行继续"))
	if line := doc.LineOf(off); line != 9 {
		t.Fatalf("line number should be 9, got %d", line)
	}
	ctx := doc.Context(off, off+len("第二行继续"), 2)
	if !strings.Contains(ctx, "第一段文字") || !strings.Contains(ctx, "引用块第一行") {
		t.Fatalf("context range is wrong:\n%s", ctx)
	}
}

func TestBlockAtInnermost(t *testing.T) {
	src := []byte(sample)
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	off := bytes.Index(src, []byte("列表项 A"))
	b := doc.BlockCovering(off)
	if b == nil {
		t.Fatal("no block located")
	}
	// The innermost hit should be TextBlock, not the outer List.
	if b.Kind != "TextBlock" {
		t.Fatalf("should hit the innermost TextBlock, got %s", b.Kind)
	}
}

func TestParseFrontmatter(t *testing.T) {
	m, err := ParseFrontmatter([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if m["aid"] != "a7f3k2" {
		t.Fatalf("failed to parse aid: %#v", m)
	}
}
