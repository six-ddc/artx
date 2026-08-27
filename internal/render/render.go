// Package render renders markdown source into HTML annotated with data-sourcepos.
//
// Owned by: W-serve.
//
// Rendering red line: this is the single source of truth for md → HTML. It
// must go through mdsrc.NewMarkdown() so that the data-sourcepos emitted in
// the HTML stays byte-for-byte consistent with the block table used to
// convert anchors. The frontend only ever consumes this package's output and
// never re-renders markdown itself.
//
// mermaid / katex ruling: the Go side does **no** special handling.
//   - mermaid: goldmark already emits <pre><code class="language-mermaid">
//     by default, so the frontend lazy-loads and runs mermaid against that
//     selector without any cooperation from Go.
//   - katex: the Go side doesn't pull in a math extension; the frontend runs
//     katex's auto-render over the rendered container and adds pre/code to
//     ignoredTags so it doesn't swallow a literal $ inside code.
//
// This satisfies the decision to ship mermaid/katex support in M1 without
// forking the md→HTML pipeline.
package render

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark/parser"

	"github.com/six-ddc/art/internal/mdsrc"
)

// Result is the output of a single render.
type Result struct {
	HTML        string
	BodyOffset  int
	Doc         *mdsrc.Document // block table; callers often reuse it to derive anchor fields
	Frontmatter map[string]any
	Title       string // frontmatter title, falling back to the first h1's text if absent
	HasMermaid  bool
	HasMath     bool
}

// Renderer holds a goldmark instance and is safe for concurrent use.
type Renderer struct{}

// New returns a Renderer.
func New() *Renderer { return &Renderer{} }

// Render renders markdown source.
//
// Steps:
//  1. mdsrc.SplitFrontmatter cuts off the frontmatter and records BodyOffset.
//  2. mdsrc.NewContext(BodyOffset) builds a parser.Context.
//  3. md.Convert(body, buf, parser.WithContext(ctx)) does the conversion.
//  4. mdsrc.Parse builds the block table and stores it in Result.Doc.
//  5. The block table is scanned to determine HasMermaid (a code block with
//     Language=="mermaid") and HasMath (body contains a '$' outside of a
//     code block).
func (r *Renderer) Render(src []byte) (*Result, error) {
	_, bodyOffset := mdsrc.SplitFrontmatter(src)
	frontmatter, err := mdsrc.ParseFrontmatter(src)
	if err != nil {
		return nil, fmt.Errorf("render: frontmatter: %w", err)
	}

	body := src[bodyOffset:]
	md := mdsrc.NewMarkdown()
	var buf bytes.Buffer
	if err := md.Convert(body, &buf, parser.WithContext(mdsrc.NewContext(bodyOffset))); err != nil {
		return nil, fmt.Errorf("render: convert: %w", err)
	}

	doc, err := mdsrc.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("render: parse: %w", err)
	}

	var hasMermaid, hasMath bool
	for _, b := range doc.Blocks {
		if b.Kind == "FencedCodeBlock" || b.Kind == "CodeBlock" {
			if b.Language == "mermaid" {
				hasMermaid = true
			}
			continue
		}
		if strings.ContainsRune(doc.BlockMap(&b).Text, '$') {
			hasMath = true
		}
	}

	title, _ := frontmatter["title"].(string)
	if title == "" {
		for _, b := range doc.Blocks {
			if b.Kind == "Heading" && b.Level == 1 {
				title = strings.TrimSpace(doc.BlockMap(&b).Text)
				break
			}
		}
	}

	return &Result{
		HTML:        buf.String(),
		BodyOffset:  bodyOffset,
		Doc:         doc,
		Frontmatter: frontmatter,
		Title:       title,
		HasMermaid:  hasMermaid,
		HasMath:     hasMath,
	}, nil
}

// Sanitize records whether the rendered output needs sanitizing: it does not.
//
// The reasoning is written down here so nobody has to relitigate it: the
// markdown content comes from a local agent, trusted the same as a file the
// user wrote themselves; serve listens on 127.0.0.1 by default; the html
// artifact runs inside a sandboxed iframe. So enabling goldmark's WithUnsafe
// to allow raw HTML is deliberate, with the outer shell page's CSP as the
// backstop.
const Sanitize = false
