package docdiff

import (
	"strings"
	"testing"

	"github.com/six-ddc/artx/internal/api"
)

func page(body, style string) []byte {
	return []byte("<!doctype html><html><head><title>t</title><style>" + style +
		"</style></head><body>" + body + "</body></html>")
}

func elemOps(t *testing.T, srcA, srcB []byte) ([]api.DiffElement, bool) {
	t.Helper()
	elems, chrome, err := ElementOps(srcA, srcB)
	if err != nil {
		t.Fatal(err)
	}
	return elems, chrome
}

func TestElementOpsIdentical(t *testing.T) {
	src := page(`<div data-aid="e1"><p data-aid="e2">hello</p></div>`, "p{color:red}")
	elems, chrome := elemOps(t, src, src)
	if len(elems) != 0 || chrome {
		t.Fatalf("elems = %v chrome = %v, want empty/false", elems, chrome)
	}
}

func TestElementOpsChangedDeepestAttribution(t *testing.T) {
	srcA := page(`<div data-aid="e1"><h1 data-aid="e2">Title</h1><p data-aid="e3">old text</p></div>`, "")
	srcB := page(`<div data-aid="e1"><h1 data-aid="e2">Title</h1><p data-aid="e3">new text</p></div>`, "")
	elems, chrome := elemOps(t, srcA, srcB)
	// Only the paragraph that actually changed lights up; its parent div's
	// own content (two aid placeholders) is identical.
	if len(elems) != 1 || elems[0].Op != api.DiffChanged || elems[0].AID != "e3" {
		t.Fatalf("elems = %+v, want single changed e3", elems)
	}
	if chrome {
		t.Fatal("chrome_changed must stay false for a content edit")
	}
}

func TestElementOpsParentOwnContentChange(t *testing.T) {
	srcA := page(`<div data-aid="e1">prefix <p data-aid="e2">kept</p></div>`, "")
	srcB := page(`<div data-aid="e1">changed prefix <p data-aid="e2">kept</p></div>`, "")
	elems, _ := elemOps(t, srcA, srcB)
	if len(elems) != 1 || elems[0].AID != "e1" || elems[0].Op != api.DiffChanged {
		t.Fatalf("elems = %+v, want single changed e1", elems)
	}
}

func TestElementOpsAddedAndRemoved(t *testing.T) {
	srcA := page(`<p data-aid="e1">stay</p><p data-aid="e2">going away</p>`, "")
	srcB := page(`<p data-aid="e1">stay</p><p data-aid="e3">brand new</p>`, "")
	elems, _ := elemOps(t, srcA, srcB)
	if len(elems) != 2 {
		t.Fatalf("elems = %+v, want added+removed", elems)
	}
	if elems[0].Op != api.DiffAdded || elems[0].AID != "e3" {
		t.Fatalf("first op = %+v, want added e3", elems[0])
	}
	rm := elems[1]
	if rm.Op != api.DiffRemoved || rm.AID != "e2" {
		t.Fatalf("second op = %+v, want removed e2", rm)
	}
	if !strings.Contains(rm.HTML, "going away") || !strings.Contains(rm.HTML, `data-aid="e2"`) {
		t.Fatalf("removed html = %q, want the old outerHTML", rm.HTML)
	}
}

func TestElementOpsAddedChildDoesNotFlagParent(t *testing.T) {
	srcA := page(`<ul data-aid="lst"><li data-aid="li1">a</li><li data-aid="li2">b</li></ul>`, "")
	srcB := page(`<ul data-aid="lst"><li data-aid="li1">a</li><li data-aid="li2">b</li><li data-aid="li3">c</li></ul>`, "")
	elems, _ := elemOps(t, srcA, srcB)
	// The new li is its own added op; its placeholder must not drag the
	// parent list into "changed".
	if len(elems) != 1 || elems[0].Op != api.DiffAdded || elems[0].AID != "li3" {
		t.Fatalf("elems = %+v, want single added li3", elems)
	}
}

func TestElementOpsRemovedChildDoesNotFlagParent(t *testing.T) {
	srcA := page(`<ul data-aid="lst"><li data-aid="li1">a</li><li data-aid="li2">b</li></ul>`, "")
	srcB := page(`<ul data-aid="lst"><li data-aid="li1">a</li></ul>`, "")
	elems, _ := elemOps(t, srcA, srcB)
	if len(elems) != 1 || elems[0].Op != api.DiffRemoved || elems[0].AID != "li2" {
		t.Fatalf("elems = %+v, want single removed li2", elems)
	}
	if !strings.Contains(elems[0].HTML, ">b</li>") {
		t.Fatalf("removed html = %q", elems[0].HTML)
	}
}

func TestElementOpsSubtreeRemovalReportsOutermostOnly(t *testing.T) {
	srcA := page(`<div data-aid="wrap"><p data-aid="keep">stay</p><section data-aid="sec"><h2 data-aid="h">Head</h2><p data-aid="p">body</p></section></div>`, "")
	srcB := page(`<div data-aid="wrap"><p data-aid="keep">stay</p></div>`, "")
	elems, _ := elemOps(t, srcA, srcB)
	// One removed op for the outermost section — not three — and the shared
	// wrapper must not turn changed either.
	if len(elems) != 1 || elems[0].Op != api.DiffRemoved || elems[0].AID != "sec" {
		t.Fatalf("elems = %+v, want single removed sec", elems)
	}
	// The attached outerHTML is the whole subtree, descendants included.
	if !strings.Contains(elems[0].HTML, "Head") || !strings.Contains(elems[0].HTML, "body") {
		t.Fatalf("removed html = %q, want the full subtree", elems[0].HTML)
	}
}

func TestElementOpsSubtreeAdditionReportsOutermostOnly(t *testing.T) {
	srcA := page(`<div data-aid="wrap"><p data-aid="keep">stay</p></div>`, "")
	srcB := page(`<div data-aid="wrap"><p data-aid="keep">stay</p><section data-aid="sec"><h2 data-aid="h">Head</h2><p data-aid="p">body</p></section></div>`, "")
	elems, _ := elemOps(t, srcA, srcB)
	if len(elems) != 1 || elems[0].Op != api.DiffAdded || elems[0].AID != "sec" {
		t.Fatalf("elems = %+v, want single added sec", elems)
	}
}

func TestElementOpsReorderFlagsParent(t *testing.T) {
	srcA := page(`<ul data-aid="lst"><li data-aid="li1">a</li><li data-aid="li2">b</li></ul>`, "")
	srcB := page(`<ul data-aid="lst"><li data-aid="li2">b</li><li data-aid="li1">a</li></ul>`, "")
	elems, _ := elemOps(t, srcA, srcB)
	// Shared children keep their placeholders, so swapping them is the
	// parent's own change; the children themselves are untouched.
	if len(elems) != 1 || elems[0].Op != api.DiffChanged || elems[0].AID != "lst" {
		t.Fatalf("elems = %+v, want single changed lst", elems)
	}
}

func TestElementOpsStyleChangeIsChromeOnly(t *testing.T) {
	srcA := page(`<p data-aid="e1">text</p>`, "p{color:red}")
	srcB := page(`<p data-aid="e1">text</p>`, "p{color:blue}")
	elems, chrome := elemOps(t, srcA, srcB)
	if len(elems) != 0 {
		t.Fatalf("elems = %+v, want none", elems)
	}
	if !chrome {
		t.Fatal("style change must set chrome_changed")
	}
}

func TestElementOpsHeadScriptChange(t *testing.T) {
	srcA := []byte(`<html><head><script>var a=1</script></head><body><p data-aid="e1">x</p></body></html>`)
	srcB := []byte(`<html><head><script>var a=2</script></head><body><p data-aid="e1">x</p></body></html>`)
	_, chrome := elemOps(t, srcA, srcB)
	if !chrome {
		t.Fatal("script change must set chrome_changed")
	}
}

func TestElementOpsNormalizationIgnoresFormatting(t *testing.T) {
	srcA := page(`<p data-aid="e1" class="a" id="x">some   text</p>`, "")
	srcB := page("<p id=\"x\" class=\"a\" data-aid=\"e1\">\n  some text\n</p>", "")
	elems, chrome := elemOps(t, srcA, srcB)
	if len(elems) != 0 || chrome {
		t.Fatalf("elems = %v chrome = %v; attribute order and whitespace must not count", elems, chrome)
	}
}
