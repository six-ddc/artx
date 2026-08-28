package mdsrc

import (
	"bytes"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Server-side syntax highlighting, chroma in class mode: the Go side only
// emits span class names (chroma's short token classes, e.g. .k .s .c); the
// colors live in styles.css keyed off the theme tokens, so light/dark
// switching stays a pure CSS concern and the binary carries no theme.
//
// The formatter deliberately emits no <pre>/<code> wrapper — those tags are
// written by codeBlockRenderer, which owns data-sourcepos and the
// language-x class the frontend keys on.
var highlightFormatter = chromahtml.New(
	chromahtml.WithClasses(true),
	chromahtml.PreventSurroundingPre(true),
)

// highlight returns class-annotated HTML for code, or nil when the language
// is unknown or tokenizing fails — the caller then falls back to plain
// escaped text, which is exactly the pre-highlighting output.
func highlight(lang string, code []byte) []byte {
	lexer := lexers.Get(lang)
	if lexer == nil {
		return nil
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, string(code))
	if err != nil {
		return nil
	}
	var buf bytes.Buffer
	if err := highlightFormatter.Format(&buf, styles.Fallback, it); err != nil {
		return nil
	}
	return buf.Bytes()
}
