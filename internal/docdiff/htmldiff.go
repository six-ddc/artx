package docdiff

import (
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/htmlaid"
)

// ElementOps classifies every data-aid-carrying element between two html
// versions: present only in the old version → removed (with its old
// outerHTML for the sidebar), only in the new → added, in both with
// differing content → changed. Unchanged elements are not reported, and an
// added/removed subtree is reported once at its outermost aid — descendants
// that appeared or vanished with it are already inside that op's content.
//
// Two decisions that keep the highlight usable:
//   - Deepest attribution: before comparing a pair, every descendant subtree
//     that carries its own aid is replaced by an <aid:xxx> placeholder — and
//     a placeholder whose aid exists on one side only is dropped outright,
//     since that subtree is already reported as its own added/removed op. So
//     a parent container only turns "changed" when content it owns directly
//     changed (a pure reorder of shared children included) — otherwise
//     editing, inserting, or deleting one paragraph would light up the whole
//     page's ancestor chain.
//   - Comparison runs on a normalized serialization (whitespace collapsed,
//     attributes sorted by name), so formatting-only rewrites stay invisible.
//
// chromeChanged reports head/style/script differences separately: those have
// no aid and no element to highlight, and the UI shows a banner instead.
func ElementOps(srcA, srcB []byte) (elems []api.DiffElement, chromeChanged bool, err error) {
	docA, err := htmlaid.Parse(srcA)
	if err != nil {
		return nil, false, err
	}
	docB, err := htmlaid.Parse(srcB)
	if err != nil {
		return nil, false, err
	}

	orderA, mapA := collectAIDs(docA)
	orderB, mapB := collectAIDs(docB)

	// Aids present on one side only are reported as added/removed below; a
	// parent's comparison must not see their placeholders, otherwise every
	// structural insert/delete would also flag the whole ancestor chain as
	// changed. Placeholders for shared aids stay — that's what keeps a pure
	// reorder of children attributed to the parent.
	onlyA, onlyB := map[string]bool{}, map[string]bool{}
	for _, aid := range orderA {
		if _, ok := mapB[aid]; !ok {
			onlyA[aid] = true
		}
	}
	for _, aid := range orderB {
		if _, ok := mapA[aid]; !ok {
			onlyB[aid] = true
		}
	}

	for _, aid := range orderB {
		if onlyB[aid] {
			// A whole added subtree is one op: descendants that came along
			// with it are already inside the outermost element's report.
			if !hasOnlyAncestor(mapB[aid], onlyB) {
				elems = append(elems, api.DiffElement{Op: api.DiffAdded, AID: aid})
			}
			continue
		}
		if normalizeElement(mapA[aid], onlyA) != normalizeElement(mapB[aid], onlyB) {
			elems = append(elems, api.DiffElement{Op: api.DiffChanged, AID: aid})
		}
	}
	for _, aid := range orderA {
		if !onlyA[aid] || hasOnlyAncestor(mapA[aid], onlyA) {
			continue
		}
		raw, rerr := htmlaid.Render(mapA[aid])
		if rerr != nil {
			raw = nil
		}
		elems = append(elems, api.DiffElement{Op: api.DiffRemoved, AID: aid, HTML: string(raw)})
	}

	return elems, chromeSignature(docA) != chromeSignature(docB), nil
}

// hasOnlyAncestor reports whether any ancestor element carries an aid that
// is also in the one-side-only set — i.e. this node sits inside a larger
// subtree that is itself being reported as added/removed, so reporting it
// again would only duplicate content the outermost op already carries.
func hasOnlyAncestor(n *html.Node, only map[string]bool) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && only[attrVal(p, htmlaid.AIDAttr)] {
			return true
		}
	}
	return false
}

// collectAIDs returns the document's aid-bearing elements in pre-order.
// Injection guarantees uniqueness; on a hand-edited duplicate the first
// occurrence wins, matching htmlaid.FindByAID.
func collectAIDs(doc *html.Node) (order []string, byAID map[string]*html.Node) {
	byAID = map[string]*html.Node{}
	var rec func(*html.Node)
	rec = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if aid := attrVal(n, htmlaid.AIDAttr); aid != "" {
				if _, dup := byAID[aid]; !dup {
					order = append(order, aid)
					byAID[aid] = n
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(doc)
	return order, byAID
}

// normalizeElement serializes an element for comparison: descendant aid
// subtrees become placeholders, attributes are sorted, whitespace is
// collapsed, comments are dropped. Descendants whose aid is in ignore are
// skipped entirely — they exist on this side only and are already reported
// as their own added/removed op.
func normalizeElement(n *html.Node, ignore map[string]bool) string {
	var sb strings.Builder
	writeNormalized(&sb, n, true, ignore)
	return sb.String()
}

func writeNormalized(sb *strings.Builder, n *html.Node, root bool, ignore map[string]bool) {
	switch n.Type {
	case html.TextNode:
		sb.WriteString(collapseWS(n.Data))
	case html.CommentNode:
		// invisible in the rendered page, never a content change
	case html.ElementNode:
		if !root {
			if aid := attrVal(n, htmlaid.AIDAttr); aid != "" {
				if !ignore[aid] {
					sb.WriteString("<aid:" + aid + ">")
				}
				return
			}
		}
		sb.WriteString("<" + n.Data)
		attrs := append([]html.Attribute(nil), n.Attr...)
		sort.Slice(attrs, func(i, j int) bool { return attrs[i].Key < attrs[j].Key })
		for _, a := range attrs {
			sb.WriteString(" " + a.Key + "=" + strconv.Quote(a.Val))
		}
		sb.WriteString(">")
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			writeNormalized(sb, c, false, ignore)
		}
		sb.WriteString("</" + n.Data + ">")
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			writeNormalized(sb, c, false, ignore)
		}
	}
}

// collapseWS collapses whitespace runs to single spaces and trims the edges;
// a whitespace-only text node normalizes to nothing. Pretty-printing changes
// are thereby invisible to the comparison, matching what a viewer sees.
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// chromeSignature fingerprints the parts of the document element ops cannot
// attribute: the whole <head> plus every style/script subtree anywhere in
// the tree (head entries count twice, which is harmless for an equality
// check).
func chromeSignature(doc *html.Node) string {
	var sb strings.Builder
	var rec func(*html.Node)
	rec = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "head" || n.Data == "style" || n.Data == "script") {
			writeNormalized(&sb, n, true, nil)
			sb.WriteString("\x00")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(doc)
	return sb.String()
}

func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
