package mdsrc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuin/goldmark/parser"
)

func convert(t *testing.T, src string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := NewMarkdown().Convert([]byte(src), &buf, parser.WithContext(NewContext(0))); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestAlertRendering(t *testing.T) {
	html := convert(t, "> [!NOTE]\n> 提示正文，含 **加粗**。\n")
	for _, want := range []string{
		`class="art-alert art-alert-note"`,
		`data-alert="note"`,
		`data-sourcepos=`,
		`class="art-alert-title"`,
		`<span>Note</span>`,
		"提示正文，含 <strong>加粗</strong>。",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in:\n%s", want, html)
		}
	}
	// The marker must not leak into the rendered body.
	if strings.Contains(html, "[!NOTE]") {
		t.Fatalf("marker not stripped:\n%s", html)
	}
}

func TestAlertKindsAndCase(t *testing.T) {
	for marker, kind := range map[string]string{
		"[!TIP]": "tip", "[!important]": "important",
		"[!Warning]": "warning", "[!CAUTION]": "caution",
	} {
		html := convert(t, "> "+marker+"\n> 内容\n")
		if !strings.Contains(html, `data-alert="`+kind+`"`) {
			t.Fatalf("marker %s did not yield kind %s:\n%s", marker, kind, html)
		}
	}
}

// GitHub rules: an unknown kind, or trailing text on the marker line, leaves
// the blockquote as a plain quote with the text intact.
func TestAlertNonMatches(t *testing.T) {
	for _, src := range []string{
		"> [!FOO]\n> 内容\n",
		"> [!NOTE] 同一行还有字\n",
		"> **[!NOTE]**\n> 内容\n",
		"> 普通引用\n",
	} {
		html := convert(t, src)
		if strings.Contains(html, "art-alert") {
			t.Fatalf("should not be an alert: %q ->\n%s", src, html)
		}
	}
	if html := convert(t, "> [!FOO]\n"); !strings.Contains(html, "[!FOO]") {
		t.Fatalf("unknown marker text must be preserved:\n%s", html)
	}
}

// A marker-only alert keeps an (empty) paragraph as its sourcepos carrier;
// an alert with more content drops the empty marker paragraph.
func TestAlertMarkerOnlyKeepsSourcepos(t *testing.T) {
	html := convert(t, "> [!TIP]\n")
	if !strings.Contains(html, "data-sourcepos") {
		t.Fatalf("marker-only alert lost its sourcepos:\n%s", html)
	}
	html = convert(t, "> [!TIP]\n>\n> 独立段落\n")
	if strings.Contains(html, "<p data-sourcepos></p>") || strings.Count(html, "<p data-sourcepos=") != 1 {
		t.Fatalf("empty marker paragraph should be dropped:\n%s", html)
	}
}

// The block table must be byte-identical between an alert and the same
// blockquote without a recognized marker: same kinds, and spans that still
// cover the marker line (block-level editing edits the full source).
func TestAlertBlockTableInvariant(t *testing.T) {
	alert := "前文。\n\n> [!NOTE]\n> 卡片正文第一行。\n> 第二行。\n\n后文。\n"
	plain := "前文。\n\n> [!XOTE]\n> 卡片正文第一行。\n> 第二行。\n\n后文。\n"
	da, err := Parse([]byte(alert))
	if err != nil {
		t.Fatal(err)
	}
	dp, err := Parse([]byte(plain))
	if err != nil {
		t.Fatal(err)
	}
	if len(da.Blocks) != len(dp.Blocks) {
		t.Fatalf("block count drifted: alert=%d plain=%d", len(da.Blocks), len(dp.Blocks))
	}
	for i := range da.Blocks {
		a, p := da.Blocks[i], dp.Blocks[i]
		if a.Kind != p.Kind || a.Start != p.Start || a.End != p.End || a.Depth != p.Depth {
			t.Fatalf("block %d drifted: alert=%+v plain=%+v", i, a, p)
		}
	}
	// Quote-anchoring inside the alert body must keep working: a quote from
	// the body maps back to the exact source bytes via BlockMap.
	var para *Block
	for i := range da.Blocks {
		if da.Blocks[i].Kind == "Paragraph" && da.Blocks[i].Depth == 1 {
			para = &da.Blocks[i]
			break
		}
	}
	if para == nil {
		t.Fatal("alert body paragraph not found in block table")
	}
	m := da.BlockMap(para)
	quote := "卡片正文第一行。"
	idx := strings.Index(m.Text, quote)
	if idx < 0 {
		t.Fatalf("quote not found in block text %q", m.Text)
	}
	s, e := m.Range(idx, idx+len(quote))
	if got := alert[s:e]; got != quote {
		t.Fatalf("quote mapped to %q, want %q", got, quote)
	}
}

func TestHighlightedCodeBlock(t *testing.T) {
	html := convert(t, "```go\nfunc main() {}\n```\n")
	for _, want := range []string{
		`<pre data-sourcepos=`,
		`class="language-go chroma"`,
		`<span class="kd">func</span>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in:\n%s", want, html)
		}
	}
}

// Unknown languages and bare fences fall back to the exact pre-highlighting
// output; mermaid must stay a plain block for the frontend to swallow.
func TestHighlightFallback(t *testing.T) {
	html := convert(t, "```\n<plain> &\n```\n")
	if !strings.Contains(html, "&lt;plain&gt; &amp;\n") || strings.Contains(html, "chroma") {
		t.Fatalf("bare fence must render escaped plain text:\n%s", html)
	}
	html = convert(t, "```mermaid\ngraph TD; A-->B;\n```\n")
	if !strings.Contains(html, `<code class="language-mermaid">graph TD; A--&gt;B;`) {
		t.Fatalf("mermaid block must stay untouched:\n%s", html)
	}
}

func TestEmojiShortcode(t *testing.T) {
	html := convert(t, "发布 :rocket: 于 12:30\n")
	if !strings.Contains(html, "&#x1f680;") && !strings.Contains(html, "🚀") {
		t.Fatalf("emoji shortcode not rendered:\n%s", html)
	}
	if !strings.Contains(html, "12:30") {
		t.Fatalf("plain colons must survive:\n%s", html)
	}
}
