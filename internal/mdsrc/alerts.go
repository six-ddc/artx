package mdsrc

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// GitHub-style alerts: a blockquote whose first line is exactly `[!NOTE]`
// (or TIP / IMPORTANT / WARNING / CAUTION, case-insensitive) renders as a
// callout card instead of a plain quote.
//
// Implemented in-house instead of pulling a third-party goldmark extension
// because the anchor system imposes two constraints nobody else honors:
//   - The node stays a Blockquote. collectBlocks and sourcePosTransformer
//     see the same AST shape as a plain quote, so the block table and
//     data-sourcepos are byte-for-byte identical to the pre-alert pipeline
//     and existing comment anchors survive.
//   - Only the marker's *inline* nodes are dropped. The paragraph's Lines()
//     still cover the marker line, so the block's sourcepos span includes
//     `[!NOTE]` and block-level editing round-trips the full source.

type alertKind struct {
	name  string // class suffix and data-alert value
	title string // rendered label
	icon  string // inline SVG (lucide outline, licensed ISC)
}

const alertSVGOpen = `<svg class="art-alert-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">`

var alertKinds = map[string]alertKind{
	"NOTE": {name: "note", title: "Note", icon: alertSVGOpen +
		`<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>`},
	"TIP": {name: "tip", title: "Tip", icon: alertSVGOpen +
		`<path d="M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5"/><path d="M9 18h6"/><path d="M10 22h4"/></svg>`},
	"IMPORTANT": {name: "important", title: "Important", icon: alertSVGOpen +
		`<path d="M22 17a2 2 0 0 1-2 2H6.828a2 2 0 0 0-1.414.586l-2.202 2.202A.71.71 0 0 1 2 21.286V5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2z"/><path d="M12 15h.01"/><path d="M12 7v4"/></svg>`},
	"WARNING": {name: "warning", title: "Warning", icon: alertSVGOpen +
		`<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/></svg>`},
	"CAUTION": {name: "caution", title: "Caution", icon: alertSVGOpen +
		`<path d="M12 16h.01"/><path d="M12 8v4"/><path d="M15.312 2a2 2 0 0 1 1.414.586l4.688 4.688A2 2 0 0 1 22 8.688v6.624a2 2 0 0 1-.586 1.414l-4.688 4.688a2 2 0 0 1-1.414.586H8.688a2 2 0 0 1-1.414-.586l-4.688-4.688A2 2 0 0 1 2 15.312V8.688a2 2 0 0 1 .586-1.414l4.688-4.688A2 2 0 0 1 8.688 2z"/></svg>`},
}

// alertMarker matches a whole first line; GitHub requires the marker alone
// on its line, so trailing text on the same line leaves a plain blockquote.
var alertMarker = regexp.MustCompile(`^\[!([A-Za-z]+)\]$`)

type alertTransformer struct{}

func (t *alertTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	source := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Kind() != ast.KindBlockquote {
			return ast.WalkContinue, nil
		}
		para, ok := n.FirstChild().(*ast.Paragraph)
		if !ok || para.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		firstLine := para.Lines().At(0)
		m := alertMarker.FindSubmatch(bytes.TrimSpace(firstLine.Value(source)))
		if m == nil {
			return ast.WalkContinue, nil
		}
		kind, ok := alertKinds[strings.ToUpper(string(m[1]))]
		if !ok {
			return ast.WalkContinue, nil
		}
		// Drop the marker's inline nodes: every Text lying inside the first
		// line. The line's softbreak is a flag on the last of them, so the
		// break disappears with the nodes. `[!X]` parses to Texts only (the
		// brackets never form a link), so hitting a non-Text means the line
		// isn't a pure marker after all — bail without transforming.
		var markerNodes []ast.Node
		for c := para.FirstChild(); c != nil; c = c.NextSibling() {
			txt, isText := c.(*ast.Text)
			if !isText || txt.Segment.Start < firstLine.Start || txt.Segment.Stop > firstLine.Stop {
				break
			}
			markerNodes = append(markerNodes, c)
		}
		if len(markerNodes) == 0 {
			return ast.WalkContinue, nil
		}
		for _, c := range markerNodes {
			para.RemoveChild(para, c)
		}
		// A marker-only paragraph is dropped entirely — unless it is the
		// blockquote's sole child, in which case it stays as the carrier of
		// the block's sourcepos (a span-less container would lose its
		// data-sourcepos and stop being an edit target).
		if para.ChildCount() == 0 && n.ChildCount() > 1 {
			n.RemoveChild(n, para)
		}
		n.SetAttributeString("class", []byte("art-alert art-alert-"+kind.name))
		n.SetAttributeString("data-alert", []byte(kind.name))
		return ast.WalkContinue, nil
	})
}

// alertRenderer overrides KindBlockquote for both alert and plain quotes:
// goldmark's default blockquote renderer already emits attributes, but the
// alert title row has to be injected right after the opening tag, which only
// a renderer override can do.
type alertRenderer struct{}

func (r *alertRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindBlockquote, r.render)
}

func (r *alertRenderer) render(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("</blockquote>\n")
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString("<blockquote")
	if n.Attributes() != nil {
		html.RenderAttributes(w, n, html.BlockquoteAttributeFilter)
	}
	_, _ = w.WriteString(">\n")
	if v, ok := n.AttributeString("data-alert"); ok {
		if name, ok := v.([]byte); ok {
			if kind, ok := alertKinds[strings.ToUpper(string(name))]; ok {
				_, _ = w.WriteString(`<p class="art-alert-title">`)
				_, _ = w.WriteString(kind.icon)
				_, _ = w.WriteString(`<span>`)
				_, _ = w.WriteString(kind.title)
				_, _ = w.WriteString("</span></p>\n")
			}
		}
	}
	return ast.WalkContinue, nil
}
