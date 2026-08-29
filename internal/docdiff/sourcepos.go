package docdiff

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// TopLevelBySourcepos indexes a rendered md fragment's **top-level** elements
// by their data-sourcepos value ("start:end"), returning each element's
// outerHTML. The diff handler uses it to pull a removed block's HTML out of
// the old version rendered whole (so reference links and footnotes resolve
// against their own document).
//
// Only top-level nodes are indexed on purpose: data-sourcepos values are not
// unique across depths — a blockquote and the single paragraph inside it
// span the same source range — and a whole-document attribute search could
// hand back the inner fragment.
func TopLevelBySourcepos(rendered string) map[string]string {
	body := &html.Node{Type: html.ElementNode, DataAtom: atom.Body, Data: "body"}
	nodes, err := html.ParseFragment(strings.NewReader(rendered), body)
	if err != nil {
		return nil
	}
	idx := map[string]string{}
	for _, n := range nodes {
		if n.Type != html.ElementNode {
			continue
		}
		pos := attrVal(n, "data-sourcepos")
		if pos == "" {
			continue
		}
		if _, dup := idx[pos]; dup {
			continue
		}
		var buf bytes.Buffer
		if html.Render(&buf, n) == nil {
			idx[pos] = buf.String()
		}
	}
	return idx
}
