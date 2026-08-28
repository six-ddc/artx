// Package htmlaid handles injecting data-aid element ids into html artifacts
// and reading/writing individual elements by id.
//
// Owner: W-anchor. Built on golang.org/x/net/html.
//
// Injection rules (design doc §8, must be idempotent):
//   - Only elements in BlockTags (block-level/semantic elements) get an id;
//     span-level elements are never touched — fine-grained anchoring is
//     handled by quote instead.
//   - Elements that already carry a data-aid keep it as-is; ids are never
//     reassigned.
//   - The file is only rewritten when this call actually added a new id,
//     to avoid the watcher self-triggering.
//   - The document id lives in <meta name="aid" content="...">.
package htmlaid

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/six-ddc/artx/internal/idgen"
)

// AIDAttr is the name of the element anchor attribute.
const AIDAttr = "data-aid"

// ReviewerScriptPath is the URL of the reviewer script injected into the
// iframe (served via embed on the serve side).
// The /_artx/ prefix keeps it fully isolated from vault content paths.
const ReviewerScriptPath = "/_artx/reviewer.js"

// BlockTags is the set of tags that get a data-aid assigned.
// Keep this list stable: changing it causes existing documents to be
// re-assigned en masse, orphaning every comment on them.
var BlockTags = map[string]bool{
	"section": true, "article": true, "header": true, "footer": true,
	"main": true, "aside": true, "nav": true, "div": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"p": true, "ul": true, "ol": true, "li": true, "dl": true, "dt": true, "dd": true,
	"table": true, "thead": true, "tbody": true, "tr": true, "th": true, "td": true,
	"blockquote": true, "pre": true, "figure": true, "figcaption": true,
	"form": true, "fieldset": true, "button": true, "img": true, "video": true,
	"audio": true, "canvas": true, "svg": true, "hr": true,
}

// InjectResult describes the outcome of one injection pass.
type InjectResult struct {
	Changed bool     // whether any id was added (if false, the caller must NOT rewrite the file)
	Added   []string // newly assigned aids
	Output  []byte   // the full rewritten document; equivalent to the input when Changed is false
}

// Inject assigns ids to block-level elements that are missing a data-aid. Idempotent.
func Inject(src []byte) (InjectResult, error) {
	doc, err := Parse(src)
	if err != nil {
		return InjectResult{}, err
	}

	// First collect every existing id so newly assigned ones avoid them —
	// aids must be unique within a single document.
	used := map[string]bool{}
	walk(doc, func(n *html.Node) {
		if n.Type == html.ElementNode {
			if v := attr(n, AIDAttr); v != "" {
				used[v] = true
			}
		}
	})

	var added []string
	walk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || !BlockTags[n.Data] || attr(n, AIDAttr) != "" {
			return
		}
		id := idgen.ElementID()
		for used[id] {
			id = idgen.ElementID()
		}
		used[id] = true
		setAttr(n, AIDAttr, id)
		added = append(added, id)
	})

	if len(added) == 0 {
		// Nothing was added, so return the input bytes verbatim. x/net/html
		// fills in <html>/<head>/<body>, so a re-serialized document is
		// almost never byte-identical to the input; rewriting anyway would
		// make the watcher see a change on every single pass.
		return InjectResult{Changed: false, Output: src}, nil
	}
	out, err := Render(doc)
	if err != nil {
		return InjectResult{}, err
	}
	return InjectResult{Changed: true, Added: added, Output: out}, nil
}

// Parse parses a complete html document.
//
// Note that x/net/html fills in the <html><head><body> structure, so
// Render(Parse(x)) != x. This is exactly why Inject must decide "did this
// actually add an id" via Changed rather than by comparing bytes: a naive
// rewrite would make the watcher see a change on every pass and
// self-trigger.
func Parse(src []byte) (*html.Node, error) {
	return html.Parse(bytes.NewReader(src))
}

// Render serializes a node tree back to bytes.
func Render(n *html.Node) ([]byte, error) {
	var buf bytes.Buffer
	if err := html.Render(&buf, n); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FindByAID looks up the element carrying the given data-aid in the tree.
func FindByAID(root *html.Node, aid string) *html.Node {
	if aid == "" {
		return nil
	}
	return find(root, func(n *html.Node) bool {
		return n.Type == html.ElementNode && attr(n, AIDAttr) == aid
	})
}

// ExtractDocAID reads <meta name="aid" content="...">, returning an empty string if absent.
func ExtractDocAID(src []byte) (string, error) {
	doc, err := Parse(src)
	if err != nil {
		return "", err
	}
	if m := findAIDMeta(doc); m != nil {
		return attr(m, "content"), nil
	}
	return "", nil
}

// SetDocAID writes or updates <meta name="aid">, adding <head> if needed.
func SetDocAID(src []byte, aid string) ([]byte, error) {
	doc, err := Parse(src)
	if err != nil {
		return nil, err
	}
	if m := findAIDMeta(doc); m != nil {
		setAttr(m, "content", aid)
		return Render(doc)
	}
	head := findElement(doc, "head")
	if head == nil {
		// html.Parse always synthesizes a head; this is just a fallback in
		// case the caller passed in a fragment tree.
		root := findElement(doc, "html")
		if root == nil {
			root = doc
		}
		head = &html.Node{Type: html.ElementNode, DataAtom: atom.Head, Data: "head"}
		if root.FirstChild != nil {
			root.InsertBefore(head, root.FirstChild)
		} else {
			root.AppendChild(head)
		}
	}
	meta := &html.Node{
		Type: html.ElementNode, DataAtom: atom.Meta, Data: "meta",
		Attr: []html.Attribute{{Key: "name", Val: "aid"}, {Key: "content", Val: aid}},
	}
	head.AppendChild(meta)
	return Render(doc)
}

// Title reads the <title> text.
func Title(src []byte) (string, error) {
	doc, err := Parse(src)
	if err != nil {
		return "", err
	}
	t := findElement(doc, "title")
	if t == nil {
		return "", nil
	}
	return strings.TrimSpace(textOf(t)), nil
}

// ElementText returns the plain-text content of the element with the given
// aid, used to show the anchored target in the comment list.
func ElementText(src []byte, aid string) (string, error) {
	doc, err := Parse(src)
	if err != nil {
		return "", err
	}
	n := FindByAID(doc, aid)
	if n == nil {
		return "", nil
	}
	return strings.Join(strings.Fields(textOf(n)), " "), nil
}

// ReviewerOptions controls the script tag injected into the iframe.
type ReviewerOptions struct {
	ScriptPath string // defaults to ReviewerScriptPath
	Mode       string // review | browse, written into data-art-mode
	Disabled   bool   // true means the --raw escape hatch: inject no script at all
}

// InjectReviewer inserts the reviewer script tag right before </body>.
// This only happens in the response stream and is never written back to
// the source file — the source file should never carry any trace of art's
// runtime.
func InjectReviewer(src []byte, opts ReviewerOptions) ([]byte, error) {
	if opts.Disabled {
		return src, nil
	}
	doc, err := Parse(src)
	if err != nil {
		return nil, err
	}
	path := opts.ScriptPath
	if path == "" {
		path = ReviewerScriptPath
	}
	script := &html.Node{
		Type: html.ElementNode, DataAtom: atom.Script, Data: "script",
		Attr: []html.Attribute{
			{Key: "src", Val: path},
			{Key: "defer", Val: ""},
			{Key: "data-art-mode", Val: opts.Mode},
		},
	}
	target := findElement(doc, "body")
	if target == nil {
		target = doc
	}
	target.AppendChild(script)
	return Render(doc)
}

// ReplaceElementHTML replaces the subtree of the aid element with inner
// (used for M2 in-browser direct editing). inner is parsed and
// re-serialized once to avoid triggering injection.
func ReplaceElementHTML(src []byte, aid string, inner []byte) ([]byte, error) {
	doc, err := Parse(src)
	if err != nil {
		return nil, err
	}
	target := FindByAID(doc, aid)
	if target == nil {
		return nil, fmt.Errorf("htmlaid: element %s=%q not found", AIDAttr, aid)
	}
	// Parse the fragment using the target element as context: inner content
	// inside a <td> and inner content inside a <div> go through different
	// HTML parsing modes, and using the wrong context loses nodes.
	nodes, err := html.ParseFragment(bytes.NewReader(inner), target)
	if err != nil {
		return nil, err
	}
	for c := target.FirstChild; c != nil; {
		next := c.NextSibling
		target.RemoveChild(c)
		c = next
	}
	for _, n := range nodes {
		walk(n, stripReviewerResidue)
		target.AppendChild(n)
	}
	return Render(doc)
}

// reviewerClassPrefix marks classes owned by the injected reviewer script
// (art-reviewer-editing/-highlight/-flash); they are runtime state and must
// never be persisted into the source file.
const reviewerClassPrefix = "art-reviewer-"

// stripReviewerResidue removes the reviewer script's runtime state from a
// node in a committed fragment: any art-reviewer-* class is dropped, and an
// element that carried art-reviewer-editing also loses the contenteditable
// attribute the reviewer set alongside it. contenteditable WITHOUT that
// class signature is the artifact's own content and is left alone. This
// guards the single write path against any client that snapshots a subtree
// while the reviewer is still mid-edit inside it.
func stripReviewerResidue(n *html.Node) {
	if n.Type != html.ElementNode {
		return
	}
	classes := strings.Fields(attr(n, "class"))
	if len(classes) == 0 {
		return
	}
	kept := classes[:0]
	wasEditing := false
	for _, c := range classes {
		if strings.HasPrefix(c, reviewerClassPrefix) {
			if c == "art-reviewer-editing" {
				wasEditing = true
			}
			continue
		}
		kept = append(kept, c)
	}
	if len(kept) == len(classes) {
		return
	}
	filtered := n.Attr[:0]
	for _, a := range n.Attr {
		switch {
		case a.Key == "class":
			if len(kept) > 0 {
				a.Val = strings.Join(kept, " ")
				filtered = append(filtered, a)
			}
		case a.Key == "contenteditable" && wasEditing:
			// dropped: the reviewer set it when it added the class
		default:
			filtered = append(filtered, a)
		}
	}
	n.Attr = filtered
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// walk performs a pre-order traversal of the whole tree.
func walk(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

// find returns the first node satisfying pred, stopping at the first match.
func find(n *html.Node, pred func(*html.Node) bool) *html.Node {
	if pred(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if got := find(c, pred); got != nil {
			return got
		}
	}
	return nil
}

func findElement(root *html.Node, tag string) *html.Node {
	return find(root, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == tag
	})
}

func findAIDMeta(root *html.Node) *html.Node {
	return find(root, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "meta" &&
			strings.EqualFold(attr(n, "name"), "aid")
	})
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func setAttr(n *html.Node, key, val string) {
	for i := range n.Attr {
		if n.Attr[i].Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}

// textOf concatenates the plain text of a subtree, skipping script/style content.
func textOf(n *html.Node) string {
	var sb strings.Builder
	var rec func(*html.Node)
	rec = func(c *html.Node) {
		if c.Type == html.ElementNode && (c.Data == "script" || c.Data == "style") {
			return
		}
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			rec(k)
		}
	}
	rec(n)
	return sb.String()
}
