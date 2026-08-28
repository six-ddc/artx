package htmlaid

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

const sampleHTML = `<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="aid" content="a7f3k2">
<title>支付重构方案</title>
</head>
<body>
<main>
  <h1>方案概要</h1>
  <p>第一段说明文字。</p>
  <section>
    <h2>风险</h2>
    <ul><li>回滚成本</li><li>灰度周期</li></ul>
  </section>
  <button>确认</button>
  <span>这是行内元素，不该拿到 aid</span>
</main>
</body>
</html>
`

func mustInject(t *testing.T, src []byte) InjectResult {
	t.Helper()
	r, err := Inject(src)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	return r
}

func TestInjectIsIdempotent(t *testing.T) {
	first := mustInject(t, []byte(sampleHTML))
	if !first.Changed {
		t.Fatal("the first injection should add ids")
	}
	if len(first.Added) == 0 {
		t.Fatal("Added must not be empty")
	}

	second := mustInject(t, first.Output)
	if second.Changed {
		t.Fatal("Changed must be false on the second Inject")
	}
	if len(second.Added) != 0 {
		t.Fatalf("Added must be empty on the second Inject, got %v", second.Added)
	}
	if string(second.Output) != string(first.Output) {
		t.Fatal("Output must equal the input when Changed=false (otherwise the watcher would see a change on every pass)")
	}
}

func TestInjectCoversBlockTagsOnly(t *testing.T) {
	out := mustInject(t, []byte(sampleHTML)).Output
	doc, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	missing := map[string]bool{}
	var spanGotAID bool
	walk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		got := attr(n, AIDAttr) != ""
		switch {
		case BlockTags[n.Data] && !got:
			missing[n.Data] = true
		case n.Data == "span" && got:
			spanGotAID = true
		}
	})
	if len(missing) != 0 {
		t.Fatalf("block-level elements missing injection: %v", missing)
	}
	if spanGotAID {
		t.Fatal("span-level elements should not be injected with an aid")
	}
}

func TestInjectAssignsUniqueIDs(t *testing.T) {
	r := mustInject(t, []byte(sampleHTML))
	seen := map[string]bool{}
	for _, id := range r.Added {
		if seen[id] {
			t.Fatalf("duplicate aid: %s", id)
		}
		if len(id) != 6 {
			t.Fatalf("aid should be 6-character base36, got %q", id)
		}
		seen[id] = true
	}
}

func TestInjectKeepsExistingIDsAcrossStructureChange(t *testing.T) {
	first := mustInject(t, []byte(sampleHTML))

	// Record existing elements' aids indexed by text content, so they can
	// still be matched after the structure changes.
	before := aidByText(t, first.Output)

	// Insert a new block at the front of <main>, effectively moving button around.
	mutated := strings.Replace(string(first.Output),
		"<h1", "<div><p>新插入的段落</p></div><h1", 1)
	if mutated == string(first.Output) {
		t.Fatal("test fixture mutation failed")
	}

	second := mustInject(t, []byte(mutated))
	if !second.Changed {
		t.Fatal("the newly inserted block should be injected")
	}
	after := aidByText(t, second.Output)

	for text, id := range before {
		if got, ok := after[text]; !ok {
			t.Errorf("element %q disappeared", text)
		} else if got != id {
			t.Errorf("element %q had its aid reassigned: %s -> %s", text, id, got)
		}
	}
}

// aidByText maps an element's direct text content to its aid, only
// collecting block-level elements that have direct text.
func aidByText(t *testing.T, src []byte) map[string]string {
	t.Helper()
	doc, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out := map[string]string{}
	walk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || !BlockTags[n.Data] {
			return
		}
		if n.FirstChild == nil || n.FirstChild != n.LastChild || n.FirstChild.Type != html.TextNode {
			return
		}
		text := strings.TrimSpace(n.FirstChild.Data)
		if text == "" {
			return
		}
		out[n.Data+":"+text] = attr(n, AIDAttr)
	})
	return out
}

func TestExtractAndSetDocAID(t *testing.T) {
	got, err := ExtractDocAID([]byte(sampleHTML))
	if err != nil {
		t.Fatalf("ExtractDocAID: %v", err)
	}
	if got != "a7f3k2" {
		t.Fatalf("ExtractDocAID = %q", got)
	}

	out, err := SetDocAID([]byte(sampleHTML), "b2c9x1")
	if err != nil {
		t.Fatalf("SetDocAID: %v", err)
	}
	got, _ = ExtractDocAID(out)
	if got != "b2c9x1" {
		t.Fatalf("ExtractDocAID after overwrite = %q", got)
	}

	// A document without an existing meta tag should have one added.
	bare := []byte("<html><head><title>x</title></head><body><p>y</p></body></html>")
	out, err = SetDocAID(bare, "c3d4e5")
	if err != nil {
		t.Fatalf("SetDocAID(bare): %v", err)
	}
	got, _ = ExtractDocAID(out)
	if got != "c3d4e5" {
		t.Fatalf("ExtractDocAID after adding = %q", got)
	}
}

func TestExtractDocAIDAbsent(t *testing.T) {
	got, err := ExtractDocAID([]byte("<html><body><p>无 meta</p></body></html>"))
	if err != nil {
		t.Fatalf("ExtractDocAID: %v", err)
	}
	if got != "" {
		t.Fatalf("should return an empty string when absent, got %q", got)
	}
}

func TestTitle(t *testing.T) {
	got, err := Title([]byte(sampleHTML))
	if err != nil {
		t.Fatalf("Title: %v", err)
	}
	if got != "支付重构方案" {
		t.Fatalf("Title = %q", got)
	}
}

func TestElementText(t *testing.T) {
	out := mustInject(t, []byte(sampleHTML)).Output
	doc, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	sec := findElement(doc, "section")
	aid := attr(sec, AIDAttr)
	if aid == "" {
		t.Fatal("section did not get an aid")
	}

	got, err := ElementText(out, aid)
	if err != nil {
		t.Fatalf("ElementText: %v", err)
	}
	if !strings.Contains(got, "风险") || !strings.Contains(got, "回滚成本") {
		t.Fatalf("ElementText = %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("whitespace should be collapsed: %q", got)
	}

	if got, _ := ElementText(out, "nosuch"); got != "" {
		t.Fatalf("should return an empty string when the element is not found, got %q", got)
	}
}

func TestFindByAID(t *testing.T) {
	out := mustInject(t, []byte(sampleHTML)).Output
	doc, _ := Parse(out)
	h1 := findElement(doc, "h1")
	aid := attr(h1, AIDAttr)

	if got := FindByAID(doc, aid); got != h1 {
		t.Fatalf("FindByAID did not return the same node: %v", got)
	}
	if got := FindByAID(doc, ""); got != nil {
		t.Fatal("an empty aid should return nil")
	}
	if got := FindByAID(doc, "nosuch"); got != nil {
		t.Fatal("a non-existent aid should return nil")
	}
}

func TestInjectReviewer(t *testing.T) {
	out, err := InjectReviewer([]byte(sampleHTML), ReviewerOptions{Mode: "review"})
	if err != nil {
		t.Fatalf("InjectReviewer: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, ReviewerScriptPath) {
		t.Fatalf("script was not injected: %s", s)
	}
	if !strings.Contains(s, `data-art-mode="review"`) {
		t.Fatalf("mode was not written: %s", s)
	}
	// Must land right before </body>.
	if i, j := strings.Index(s, ReviewerScriptPath), strings.Index(s, "</body>"); i < 0 || j < 0 || i > j {
		t.Fatalf("wrong script position: script@%d body@%d", i, j)
	}

	same, err := InjectReviewer([]byte(sampleHTML), ReviewerOptions{Disabled: true})
	if err != nil {
		t.Fatalf("InjectReviewer(disabled): %v", err)
	}
	if string(same) != sampleHTML {
		t.Fatal("must return the input unchanged when Disabled")
	}
}

func TestInjectReviewerCustomPath(t *testing.T) {
	out, err := InjectReviewer([]byte(sampleHTML), ReviewerOptions{ScriptPath: "/x/y.js"})
	if err != nil {
		t.Fatalf("InjectReviewer: %v", err)
	}
	if !strings.Contains(string(out), `src="/x/y.js"`) {
		t.Fatalf("custom path did not take effect: %s", out)
	}
}

func TestReplaceElementHTML(t *testing.T) {
	out := mustInject(t, []byte(sampleHTML)).Output
	doc, _ := Parse(out)
	aid := attr(findElement(doc, "h1"), AIDAttr)

	replaced, err := ReplaceElementHTML(out, aid, []byte("<em>改过的标题</em>"))
	if err != nil {
		t.Fatalf("ReplaceElementHTML: %v", err)
	}
	s := string(replaced)
	if !strings.Contains(s, "<h1 "+AIDAttr+`="`+aid+`"><em>改过的标题</em></h1>`) {
		t.Fatalf("unexpected replacement result: %s", s)
	}
	if strings.Contains(s, "方案概要") {
		t.Fatal("the original subtree should have been cleared")
	}
	// The aid must stay on the element, otherwise the thread orphans immediately.
	if got, _ := ElementText(replaced, aid); got != "改过的标题" {
		t.Fatalf("aid lost or text wrong: %q", got)
	}

	if _, err := ReplaceElementHTML(out, "nosuch", []byte("x")); err == nil {
		t.Fatal("a missing aid should return an error")
	}
}

// A fragment snapshotted while the reviewer was still mid-edit inside it
// carries the reviewer's runtime state (art-reviewer-* classes plus the
// contenteditable the script set); none of it may be persisted. A
// contenteditable that is the artifact's own (no reviewer class) must
// survive untouched.
func TestReplaceElementHTMLStripsReviewerResidue(t *testing.T) {
	out := mustInject(t, []byte(sampleHTML)).Output
	doc, _ := Parse(out)
	aid := attr(findElement(doc, "section"), AIDAttr)

	inner := `<h2>风险</h2>` +
		`<p class="art-reviewer-editing note" contenteditable="true">编辑中的段落</p>` +
		`<p class="art-reviewer-highlight art-reviewer-flash">被高亮的段落</p>` +
		`<div contenteditable="true">用户自己的可编辑区</div>`
	replaced, err := ReplaceElementHTML(out, aid, []byte(inner))
	if err != nil {
		t.Fatalf("ReplaceElementHTML: %v", err)
	}
	s := string(replaced)

	if strings.Contains(s, "art-reviewer-") {
		t.Fatalf("reviewer classes persisted: %s", s)
	}
	if !strings.Contains(s, `<p class="note">编辑中的段落</p>`) {
		t.Fatalf("non-reviewer class should survive with contenteditable stripped: %s", s)
	}
	if !strings.Contains(s, `<p>被高亮的段落</p>`) {
		t.Fatalf("an emptied class attribute should be dropped entirely: %s", s)
	}
	if !strings.Contains(s, `<div contenteditable="true">用户自己的可编辑区</div>`) {
		t.Fatalf("the artifact's own contenteditable must be left alone: %s", s)
	}
}
