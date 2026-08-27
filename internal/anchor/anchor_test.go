package anchor

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/mdsrc"
)

// Covers the three block structures most prone to bugs: inline markup,
// blockquote prefixes, and list indentation.
const sampleMD = `---
aid: a7f3k2
title: demo
---

# 演示标题

这是**粗体**文字，需要评论。

> 这是引用块的第一行
> 第二行内容在这里

- 列表第一项内容
- 列表第二项内容
`

// blockOf simulates the frontend: finds the block covering marker and uses
// its data-sourcepos value as the reported one.
func blockOf(t *testing.T, doc *mdsrc.Document, marker string) *mdsrc.Block {
	t.Helper()
	i := strings.Index(string(doc.Source), marker)
	if i < 0 {
		t.Fatalf("marker %q not found in sample source", marker)
	}
	b := doc.BlockCovering(i)
	if b == nil {
		t.Fatalf("no block covers %q", marker)
	}
	return b
}

func parseSample(t *testing.T) *mdsrc.Document {
	t.Helper()
	doc, err := mdsrc.Parse([]byte(sampleMD))
	if err != nil {
		t.Fatalf("mdsrc.Parse: %v", err)
	}
	return doc
}

// span returns marker's [start,end) range in src.
func span(t *testing.T, src, marker string) (int, int) {
	t.Helper()
	i := strings.Index(src, marker)
	if i < 0 {
		t.Fatalf("marker %q not found in source", marker)
	}
	return i, i + len(marker)
}

func TestFromSelectionBoldMarkers(t *testing.T) {
	doc := parseSample(t)
	b := blockOf(t, doc, "这是**粗体**文字")

	// The ** is gone in the rendered form, so the frontend can only report "粗体文字".
	a, err := FromSelection(doc, api.SelectionInput{
		BlockStart: b.Start,
		BlockEnd:   b.End,
		Exact:      "粗体文字",
		Before:     "这是",
	})
	if err != nil {
		t.Fatalf("FromSelection: %v", err)
	}

	wantStart, _ := span(t, sampleMD, "粗体")
	_, wantEnd := span(t, sampleMD, "文字，需要")
	wantEnd -= len("，需要")

	if a.Start != wantStart || a.End != wantEnd {
		t.Fatalf("wrong offsets: got [%d,%d) = %q, want [%d,%d) = %q",
			a.Start, a.End, sampleMD[a.Start:a.End], wantStart, wantEnd, sampleMD[wantStart:wantEnd])
	}
	if a.Exact != "粗体**文字" {
		t.Fatalf("Exact should be the source-text fragment (markup included), got %q", a.Exact)
	}
	if a.Approx {
		t.Fatal("an exact hit should not set Approx")
	}
	if a.Kind != api.AnchorText {
		t.Fatalf("Kind = %q", a.Kind)
	}
}

func TestFromSelectionInBlockquote(t *testing.T) {
	doc := parseSample(t)
	b := blockOf(t, doc, "这是引用块的第一行")

	a, err := FromSelection(doc, api.SelectionInput{
		BlockStart: b.Start,
		BlockEnd:   b.End,
		Exact:      "引用块的第一行",
		Before:     "这是",
	})
	if err != nil {
		t.Fatalf("FromSelection: %v", err)
	}

	wantStart, wantEnd := span(t, sampleMD, "引用块的第一行")
	if a.Start != wantStart || a.End != wantEnd {
		t.Fatalf("wrong offset inside blockquote: got [%d,%d) = %q, want [%d,%d)",
			a.Start, a.End, sampleMD[a.Start:a.End], wantStart, wantEnd)
	}
	// The "> " prefix isn't part of the segment; only going through BlockMap gets this right.
	if strings.Contains(a.Exact, ">") {
		t.Fatalf("anchor swallowed the blockquote prefix: %q", a.Exact)
	}
}

func TestFromSelectionInListItem(t *testing.T) {
	doc := parseSample(t)
	b := blockOf(t, doc, "列表第二项内容")

	a, err := FromSelection(doc, api.SelectionInput{
		BlockStart: b.Start,
		BlockEnd:   b.End,
		Exact:      "列表第二项内容",
	})
	if err != nil {
		t.Fatalf("FromSelection: %v", err)
	}

	wantStart, wantEnd := span(t, sampleMD, "列表第二项内容")
	if a.Start != wantStart || a.End != wantEnd {
		t.Fatalf("wrong offset inside list item: got [%d,%d) = %q, want [%d,%d)",
			a.Start, a.End, sampleMD[a.Start:a.End], wantStart, wantEnd)
	}
	if strings.Contains(a.Exact, "-") {
		t.Fatalf("anchor swallowed the list marker: %q", a.Exact)
	}
}

func TestFromSelectionFallsBackToWholeBlock(t *testing.T) {
	doc := parseSample(t)
	b := blockOf(t, doc, "这是**粗体**文字")

	a, err := FromSelection(doc, api.SelectionInput{
		BlockStart: b.Start,
		BlockEnd:   b.End,
		Exact:      "zzzzzzzzzzzz", // does not exist anywhere in the block
	})
	if err != nil {
		t.Fatalf("a failed match should not return an error, got %v", err)
	}
	if !a.Approx {
		t.Fatal("a failed in-block match should set Approx=true")
	}
	if a.Start != b.Start || a.End != b.End {
		t.Fatalf("the fallback should anchor the whole block [%d,%d), got [%d,%d)", b.Start, b.End, a.Start, a.End)
	}
}

func TestFromSelectionPrefixSuffixAreRunes(t *testing.T) {
	doc := parseSample(t)
	b := blockOf(t, doc, "这是引用块的第一行")

	a, err := FromSelection(doc, api.SelectionInput{
		BlockStart: b.Start, BlockEnd: b.End, Exact: "引用块的第一行",
	})
	if err != nil {
		t.Fatalf("FromSelection: %v", err)
	}
	if n := len([]rune(a.Prefix)); n > ContextChars {
		t.Fatalf("Prefix should be at most %d runes, got %d", ContextChars, n)
	}
	if n := len([]rune(a.Suffix)); n > ContextChars {
		t.Fatalf("Suffix should be at most %d runes, got %d", ContextChars, n)
	}
	// Truncating by byte would chop multi-byte characters in half.
	if !utf8.ValidString(a.Prefix) || !utf8.ValidString(a.Suffix) {
		t.Fatalf("prefix/suffix is not valid UTF-8: %q / %q", a.Prefix, a.Suffix)
	}
}

// ---------------------------------------------------------------------------
// Locate's three-level fallback
// ---------------------------------------------------------------------------

const locateSrc = "开头一段。\n\n需要定位的锚点文本。\n\n结尾一段。\n"

func TestLocateLevel1ExactOffset(t *testing.T) {
	start, end := span(t, locateSrc, "需要定位的锚点文本。")
	a := Anchor{Kind: api.AnchorText, Exact: "需要定位的锚点文本。", Start: start, End: end}

	m, err := Locate([]byte(locateSrc), a)
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if m.Start != start || m.End != end || m.Score != 1 {
		t.Fatalf("wrong level-1 hit: %+v, want [%d,%d) score=1", m, start, end)
	}
}

func TestLocateLevel2FullTextSearch(t *testing.T) {
	start, end := span(t, locateSrc, "需要定位的锚点文本。")
	// The offset is stale (points at the start of the file), but the content is still there.
	a := Anchor{
		Kind:   api.AnchorText,
		Exact:  "需要定位的锚点文本。",
		Prefix: "开头一段。\n\n",
		Start:  0,
		End:    len("需要定位的锚点文本。"),
	}

	m, err := Locate([]byte(locateSrc), a)
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if m.Start != start || m.End != end {
		t.Fatalf("wrong level-2 hit: %+v, want [%d,%d)", m, start, end)
	}
	if m.Score != 1 {
		t.Fatalf("a byte-identical hit should score 1, got %v", m.Score)
	}
	if m.Start == a.Start {
		t.Fatal("this case must go through the full-text search, not be hijacked by a level-1 offset hit")
	}
}

func TestLocateLevel3Bitap(t *testing.T) {
	// Two characters of the anchor text were edited: levels 1 and 2 must miss.
	changed := strings.Replace(locateSrc, "需要定位的锚点文本。", "需要定位的锚点文字！", 1)
	start, _ := span(t, changed, "需要定位的锚点文字！")

	a := Anchor{
		Kind:  api.AnchorText,
		Exact: "需要定位的锚点文本。",
		Start: start,
		End:   start + len("需要定位的锚点文本。"),
	}

	m, err := Locate([]byte(changed), a)
	if err != nil {
		t.Fatalf("level-3 bitap should have matched, got %v", err)
	}
	if m.Start != start {
		t.Fatalf("wrong level-3 hit position: %d, want %d", m.Start, start)
	}
	if m.Score <= 0 || m.Score >= 1 {
		t.Fatalf("a fuzzy hit's Score should fall in (0,1), got %v", m.Score)
	}
}

func TestLocateNoMatch(t *testing.T) {
	a := Anchor{Kind: api.AnchorText, Exact: "zzzzzzzzzzzzzzzz", Start: 0, End: 16}
	if _, err := Locate([]byte(locateSrc), a); err == nil {
		t.Fatal("text that doesn't exist at all should return ErrNoMatch")
	}
}

// ---------------------------------------------------------------------------
// Remaining exported functions
// ---------------------------------------------------------------------------

func TestQuoteRuneBoundaries(t *testing.T) {
	src := []byte(strings.Repeat("甲", 50) + "锚点" + strings.Repeat("乙", 50))
	start := len(strings.Repeat("甲", 50))
	exact, prefix, suffix := Quote(src, start, start+len("锚点"))

	if exact != "锚点" {
		t.Fatalf("exact = %q", exact)
	}
	if got := len([]rune(prefix)); got != ContextChars {
		t.Fatalf("prefix should have %d runes, got %d", ContextChars, got)
	}
	if got := len([]rune(suffix)); got != ContextChars {
		t.Fatalf("suffix should have %d runes, got %d", ContextChars, got)
	}
	if prefix != strings.Repeat("甲", ContextChars) {
		t.Fatalf("prefix = %q", prefix)
	}
}

func TestQuoteClampsOutOfRange(t *testing.T) {
	src := []byte("abc")
	exact, _, _ := Quote(src, -5, 99)
	if exact != "abc" {
		t.Fatalf("out-of-range values should be clamped, got %q", exact)
	}
}

func TestFromElement(t *testing.T) {
	a, err := FromElement(api.ElementInput{AID: "b2c9x1", Quote: "按钮文案"})
	if err != nil {
		t.Fatalf("FromElement: %v", err)
	}
	if a.Kind != api.AnchorElement || a.AID != "b2c9x1" || a.Exact != "按钮文案" {
		t.Fatalf("%+v", a)
	}
	if a.Start != 0 || a.End != 0 {
		t.Fatal("an element anchor's Start/End are meaningless and should be 0")
	}
	if _, err := FromElement(api.ElementInput{}); err == nil {
		t.Fatal("a missing aid should return an error")
	}
}

func TestEnrichFillsLineAndContext(t *testing.T) {
	doc := parseSample(t)
	start, end := span(t, sampleMD, "列表第二项内容")

	threads := []api.Thread{{
		Thread: "c00001",
		Anchor: api.ThreadAnchor{Kind: api.AnchorText, Start: start, End: end},
	}, {
		Thread: "c00002",
		Anchor: api.ThreadAnchor{Kind: api.AnchorElement, AID: "b2c9x1"},
	}}

	Enrich(doc.Source, doc, threads)

	if threads[0].Anchor.Line != doc.LineOf(start) {
		t.Fatalf("Line = %d, want %d", threads[0].Anchor.Line, doc.LineOf(start))
	}
	if !strings.Contains(threads[0].Anchor.Context, "列表第一项内容") {
		t.Fatalf("Context should contain %d lines before/after: %q", ContextLines, threads[0].Anchor.Context)
	}
	if threads[1].Anchor.Line != 0 || threads[1].Anchor.Context != "" {
		t.Fatal("an element anchor should not get its markdown-derived fields filled in")
	}
}

func TestToAPIRoundTrip(t *testing.T) {
	a := Anchor{
		Kind: api.AnchorText, Exact: "e", Prefix: "p", Suffix: "s",
		Start: 3, End: 7, Rev: "9f8e7d6", AID: "", Approx: true,
	}
	got := ToAPI(a)
	if got.Kind != a.Kind || got.Exact != a.Exact || got.Prefix != a.Prefix ||
		got.Suffix != a.Suffix || got.Start != a.Start || got.End != a.End ||
		got.Rev != a.Rev || got.Approx != a.Approx {
		t.Fatalf("fields were lost: %+v", got)
	}
	if got.Line != 0 || got.Context != "" {
		t.Fatal("Line/Context should be left for Enrich to fill in")
	}
}

func TestStripInlineMapsBack(t *testing.T) {
	cases := []struct {
		src, plain string
	}{
		{"这是**粗体**文字", "这是粗体文字"},
		{"带 `code` 的行", "带 code 的行"},
		{"~~删除线~~保留", "删除线保留"},
		{"看 [文档](https://example.com) 说明", "看 文档 说明"},
		{"图 ![alt](a.png) 后", "图 alt 后"},
		{"转义 \\* 星号", "转义 * 星号"},
		{"do_something_here 不该被剥", "do_something_here 不该被剥"},
	}
	for _, c := range cases {
		plain, idx := stripInline(c.src)
		if plain != c.plain {
			t.Errorf("stripInline(%q) = %q, want %q", c.src, plain, c.plain)
			continue
		}
		if len(idx) != len(plain)+1 {
			t.Errorf("%q: idx length %d, want %d", c.src, len(idx), len(plain)+1)
			continue
		}
		// Every stripped byte must map back to the same byte in the source.
		for i := 0; i < len(plain); i++ {
			if c.src[idx[i]] != plain[i] {
				t.Errorf("%q: idx[%d]=%d points at %q, want %q",
					c.src, i, idx[i], string(c.src[idx[i]]), string(plain[i]))
			}
		}
	}
}
