// Package mdsrc is the **single source of truth** for markdown source
// positions.
//
// It serves two consumers, which is exactly why there can only be one
// implementation:
//   - internal/anchor (W-anchor): converts a browser selection back into
//     byte offsets in the source file.
//   - internal/render (W-serve): attaches data-sourcepos to block-level
//     elements when rendering HTML.
//
// Both share the single goldmark factory NewMarkdown(), which is what
// guarantees that the data-sourcepos emitted during rendering stays
// byte-for-byte consistent with the block table anchor conversion relies on.
//
// Ownership: this package is part of the frozen architecture layer and is
// already fully implemented. Neither W-anchor nor W-serve may modify it.
//
// Core facts (empirically determined against goldmark v1.8.5; see
// blueprint.md, "Key Technical Decisions"):
//   - A block node's node.Lines() gives the [Start,Stop) byte range for
//     **each source line**, with block prefixes such as "> " and list
//     indentation already stripped out; concatenating these segments is
//     what yields the block's clean source text.
//   - Container blocks (List/ListItem/Blockquote/Table/…) have empty
//     Lines() and must have their span aggregated from their descendants.
//   - A Heading's Lines() excludes the "## " prefix; a FencedCodeBlock's
//     Lines() excludes the ``` fence lines.
//   - If frontmatter isn't stripped beforehand, goldmark parses it as a
//     ThematicBreak+Heading, so it must be cut off first, with BodyOffset
//     added back to every offset so that offsets exposed to callers are
//     always **file-absolute offsets**.
package mdsrc

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Segment is the byte range of one source line within a block, as a
// **file-absolute offset**, half-open.
type Segment struct {
	Start int
	End   int
}

// Block is the source position of a single block-level node.
type Block struct {
	Kind     string    // goldmark node Kind name, e.g. Paragraph / Heading / FencedCodeBlock
	Start    int       // start of the first segment (min over descendants for container blocks), file-absolute offset
	End      int       // end of the last segment (max over descendants for container blocks), file-absolute offset
	Level    int       // Heading level; 0 for everything else
	Depth    int       // AST depth, 0 for top-level blocks
	Segments []Segment // empty for container blocks
	Language string    // language identifier for a FencedCodeBlock
}

// Document is the result of parsing one markdown source file.
type Document struct {
	Source      []byte // the full file bytes
	BodyOffset  int    // start of the body (after frontmatter)
	Frontmatter []byte // raw frontmatter YAML (excluding the --- delimiter lines)
	Blocks      []Block

	lineStarts []int
}

// bodyOffsetKey lets the transformer read BodyOffset back out of parser.Context.
var bodyOffsetKey = parser.NewContextKey()

// WithBodyOffset sets the body offset on a parser.Context. The render package
// must set this before calling Convert, otherwise data-sourcepos will miss
// the length of the frontmatter.
func WithBodyOffset(pc parser.Context, off int) {
	pc.Set(bodyOffsetKey, off)
}

// NewContext returns a parser.Context with BodyOffset already set.
func NewContext(bodyOffset int) parser.Context {
	pc := parser.NewContext()
	WithBodyOffset(pc, bodyOffset)
	return pc
}

// NewMarkdown returns the one goldmark instance configuration used across
// the whole project.
//
// Any code that renders or parses markdown must go through it, otherwise its
// block boundaries will drift out of sync with the anchor system.
func NewMarkdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithASTTransformers(util.Prioritized(&sourcePosTransformer{}, 999)),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
			renderer.WithNodeRenderers(util.Prioritized(&codeBlockRenderer{}, 100)),
		),
	)
}

// ---------------------------------------------------------------------------
// frontmatter
// ---------------------------------------------------------------------------

// SplitFrontmatter splits off the YAML frontmatter.
// It returns the raw frontmatter (excluding the --- lines) and the body's
// starting offset. Returns (nil, 0) if there is no frontmatter.
func SplitFrontmatter(src []byte) (fm []byte, bodyOffset int) {
	if !bytes.HasPrefix(src, []byte("---\n")) && !bytes.HasPrefix(src, []byte("---\r\n")) {
		return nil, 0
	}
	start := bytes.IndexByte(src, '\n') + 1
	rest := src[start:]
	off := 0
	for off < len(rest) {
		lineEnd := bytes.IndexByte(rest[off:], '\n')
		var line []byte
		var next int
		if lineEnd < 0 {
			line, next = rest[off:], len(rest)
		} else {
			line, next = rest[off:off+lineEnd], off+lineEnd+1
		}
		trimmed := strings.TrimRight(string(line), "\r \t")
		if trimmed == "---" || trimmed == "..." {
			return rest[:off], start + next
		}
		off = next
	}
	// No closing delimiter found: treat it as having no frontmatter.
	return nil, 0
}

// ParseFrontmatter parses the frontmatter into a map. Returns an empty map
// if there is no frontmatter.
func ParseFrontmatter(src []byte) (map[string]any, error) {
	fm, _ := SplitFrontmatter(src)
	out := map[string]any{}
	if len(bytes.TrimSpace(fm)) == 0 {
		return out, nil
	}
	if err := yaml.Unmarshal(fm, &out); err != nil {
		return nil, fmt.Errorf("mdsrc: parse frontmatter: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// parsing
// ---------------------------------------------------------------------------

// Parse parses the source and builds the block table. All returned offsets
// are file-absolute.
func Parse(src []byte) (*Document, error) {
	fm, bodyOffset := SplitFrontmatter(src)
	doc := &Document{Source: src, BodyOffset: bodyOffset, Frontmatter: fm}
	doc.buildLineStarts()

	body := src[bodyOffset:]
	md := NewMarkdown()
	root := md.Parser().Parse(text.NewReader(body), parser.WithContext(NewContext(bodyOffset)))
	doc.Blocks = collectBlocks(root, body, bodyOffset)
	return doc, nil
}

func collectBlocks(root ast.Node, body []byte, base int) []Block {
	var out []Block
	depth := -1
	_ = depth
	var walk func(n ast.Node, d int)
	walk = func(n ast.Node, d int) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if c.Type() == ast.TypeBlock {
				b := Block{Kind: c.Kind().String(), Depth: d}
				if h, ok := c.(*ast.Heading); ok {
					b.Level = h.Level
				}
				if f, ok := c.(*ast.FencedCodeBlock); ok {
					b.Language = string(f.Language(body))
				}
				if lines := c.Lines(); lines != nil && lines.Len() > 0 {
					for i := 0; i < lines.Len(); i++ {
						s := lines.At(i)
						b.Segments = append(b.Segments, Segment{Start: s.Start + base, End: s.Stop + base})
					}
					b.Start = b.Segments[0].Start
					b.End = b.Segments[len(b.Segments)-1].End
				}
				out = append(out, b)
				idx := len(out) - 1
				before := len(out)
				walk(c, d+1)
				// When a container block has no segments of its own, aggregate
				// its span from the min/max of its descendants.
				if len(out[idx].Segments) == 0 && len(out) > before {
					lo, hi, ok := 0, 0, false
					for _, ch := range out[before:] {
						if len(ch.Segments) == 0 {
							continue
						}
						if !ok || ch.Start < lo {
							lo = ch.Start
						}
						if !ok || ch.End > hi {
							hi = ch.End
						}
						ok = true
					}
					if ok {
						out[idx].Start, out[idx].End = lo, hi
					}
				}
				continue
			}
			walk(c, d)
		}
	}
	walk(root, 0)
	return out
}

func (d *Document) buildLineStarts() {
	d.lineStarts = []int{0}
	for i, c := range d.Source {
		if c == '\n' {
			d.lineStarts = append(d.lineStarts, i+1)
		}
	}
}

// LineOf returns the 1-based line number a byte offset falls on.
func (d *Document) LineOf(off int) int {
	if len(d.lineStarts) == 0 {
		d.buildLineStarts()
	}
	i := sort.SearchInts(d.lineStarts, off+1) - 1
	if i < 0 {
		i = 0
	}
	return i + 1
}

// Context returns the raw source text spanning n lines before and after
// [start,end), used for the context field of `art comments --json`.
func (d *Document) Context(start, end, n int) string {
	if len(d.lineStarts) == 0 {
		d.buildLineStarts()
	}
	first := d.LineOf(start) - 1 - n
	if first < 0 {
		first = 0
	}
	last := d.LineOf(end) - 1 + n
	if last >= len(d.lineStarts) {
		last = len(d.lineStarts) - 1
	}
	from := d.lineStarts[first]
	to := len(d.Source)
	if last+1 < len(d.lineStarts) {
		to = d.lineStarts[last+1]
	}
	return strings.TrimRight(string(d.Source[from:to]), "\n")
}

// BlockAt returns the **innermost** block that fully contains [start,end);
// nil if none is found. When the browser reports a data-sourcepos that hits
// a block exactly, this degenerates into an equality lookup.
//
// When spans tie, the deeper block wins: a ListItem and the TextBlock inside
// it have identical spans, but anchoring wants the leaf block (only a
// TextBlock has segments, which is what makes in-block quote mapping
// possible).
func (d *Document) BlockAt(start, end int) *Block {
	var best *Block
	for i := range d.Blocks {
		b := &d.Blocks[i]
		if b.Start > start || end > b.End {
			continue
		}
		if best == nil {
			best = b
			continue
		}
		switch span, bestSpan := b.End-b.Start, best.End-best.Start; {
		case span < bestSpan, span == bestSpan && b.Depth > best.Depth:
			best = b
		}
	}
	return best
}

// BlockCovering returns the innermost block containing off; used to
// re-attribute a position after remapping.
func (d *Document) BlockCovering(off int) *Block {
	return d.BlockAt(off, off)
}

// ---------------------------------------------------------------------------
// in-block offset mapping
// ---------------------------------------------------------------------------

// BlockMap maps a byte index within a block's source text back to a
// file-absolute offset.
//
// A block's source text is the concatenation of its segments in order, so it
// excludes things like the "> " prefix or list indentation; an index
// obtained by matching a quote against the block text must be run through
// this to get a real file offset.
type BlockMap struct {
	Text string
	segs []Segment
}

// BlockMap builds and returns the mapper for a block. A container block
// (no segments) degenerates to the block's whole raw source range, mapped as
// an identity shift.
func (d *Document) BlockMap(b *Block) *BlockMap {
	if b == nil {
		return &BlockMap{}
	}
	if len(b.Segments) == 0 {
		if b.Start >= b.End || b.End > len(d.Source) {
			return &BlockMap{}
		}
		return &BlockMap{
			Text: string(d.Source[b.Start:b.End]),
			segs: []Segment{{Start: b.Start, End: b.End}},
		}
	}
	var sb strings.Builder
	segs := make([]Segment, 0, len(b.Segments))
	for _, s := range b.Segments {
		if s.Start < 0 || s.End > len(d.Source) || s.Start > s.End {
			continue
		}
		sb.Write(d.Source[s.Start:s.End])
		segs = append(segs, s)
	}
	return &BlockMap{Text: sb.String(), segs: segs}
}

// ToFile converts a byte index within the block's source text into a
// file-absolute offset. An out-of-range i is clamped to the block's first/
// last offset.
func (m *BlockMap) ToFile(i int) int {
	if len(m.segs) == 0 {
		return 0
	}
	if i < 0 {
		return m.segs[0].Start
	}
	acc := 0
	for _, s := range m.segs {
		n := s.End - s.Start
		if i < acc+n {
			return s.Start + (i - acc)
		}
		acc += n
	}
	return m.segs[len(m.segs)-1].End
}

// Range converts [i,j) within the block's source text into a file-absolute
// offset range.
func (m *BlockMap) Range(i, j int) (int, int) {
	return m.ToFile(i), m.ToFile(j)
}

// ---------------------------------------------------------------------------
// data-sourcepos injection
// ---------------------------------------------------------------------------

type sourcePosTransformer struct{}

func (t *sourcePosTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	base := 0
	if v, ok := pc.Get(bodyOffsetKey).(int); ok {
		base = v
	}
	var span func(n ast.Node) (int, int, bool)
	span = func(n ast.Node) (int, int, bool) {
		if l := n.Lines(); l != nil && l.Len() > 0 {
			return l.At(0).Start, l.At(l.Len() - 1).Stop, true
		}
		lo, hi, ok := 0, 0, false
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			s, e, k := span(c)
			if !k {
				continue
			}
			if !ok || s < lo {
				lo = s
			}
			if !ok || e > hi {
				hi = e
			}
			ok = true
		}
		return lo, hi, ok
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Type() != ast.TypeBlock || n.Kind() == ast.KindDocument {
			return ast.WalkContinue, nil
		}
		if s, e, ok := span(n); ok {
			n.SetAttributeString("data-sourcepos", []byte(fmt.Sprintf("%d:%d", s+base, e+base)))
		}
		return ast.WalkContinue, nil
	})
}

// codeBlockRenderer exists solely to patch one gap in goldmark's default
// renderer: it doesn't emit node attributes for (Fenced)CodeBlock, which
// means code blocks would otherwise never get a data-sourcepos. Every other
// block-level element is already rendered correctly with its attributes by
// the default renderer and needs no override.
type codeBlockRenderer struct{}

func (r *codeBlockRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.render)
	reg.Register(ast.KindCodeBlock, r.render)
}

func (r *codeBlockRenderer) render(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("</code></pre>\n")
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString("<pre")
	if sp, ok := n.AttributeString("data-sourcepos"); ok {
		if b, ok := sp.([]byte); ok {
			_, _ = w.WriteString(` data-sourcepos="`)
			_, _ = w.Write(b)
			_, _ = w.WriteString(`"`)
		}
	}
	_, _ = w.WriteString("><code")
	if f, ok := n.(*ast.FencedCodeBlock); ok {
		if lang := f.Language(source); lang != nil {
			_, _ = w.WriteString(` class="language-`)
			_, _ = w.Write(util.EscapeHTML(lang))
			_, _ = w.WriteString(`"`)
		}
	}
	_, _ = w.WriteString(">")
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		s := lines.At(i)
		_, _ = w.Write(util.EscapeHTML(s.Value(source)))
	}
	return ast.WalkSkipChildren, nil
}
