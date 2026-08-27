package server

import (
	"github.com/six-ddc/artx/internal/anchor"
	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/htmlaid"
	"github.com/six-ddc/artx/internal/mdsrc"
)

// safeFromSelection converts a browser selection into an anchor.
//
// W-anchor and W-serve are developed in parallel, so anchor.FromSelection
// could still be a panicking skeleton before integration. blueprint.md §10
// risk 3 and the team's working agreement explicitly allow degrading to a
// block-level fallback anchor (Approx=true) in that case — recover() backs
// this up so an unfinished dependency package can never take down the whole
// write path. Once the real implementation lands, this naturally falls
// through to the exact-match branch.
func (s *Server) safeFromSelection(doc *mdsrc.Document, sel api.SelectionInput) (a anchor.Anchor) {
	defer func() {
		if recover() != nil {
			a = fallbackBlockAnchor(doc, sel.BlockStart, sel.BlockEnd)
		}
	}()
	res, err := anchor.FromSelection(doc, sel)
	if err != nil {
		return fallbackBlockAnchor(doc, sel.BlockStart, sel.BlockEnd)
	}
	return res
}

// fallbackBlockAnchor is the block-level fallback anchor: it takes the
// reported block range directly as the anchor, with Approx=true.
func fallbackBlockAnchor(doc *mdsrc.Document, start, end int) anchor.Anchor {
	if doc == nil || start < 0 || end < start || end > len(doc.Source) {
		return anchor.Anchor{Kind: api.AnchorText, Approx: true}
	}
	return anchor.Anchor{
		Kind:   api.AnchorText,
		Start:  start,
		End:    end,
		Exact:  string(doc.Source[start:end]),
		Approx: true,
	}
}

// safeFromElement converts an html element selection into an anchor,
// applying the same degrade-to-fallback treatment to anchor.FromElement.
func (s *Server) safeFromElement(el api.ElementInput) (a anchor.Anchor) {
	defer func() {
		if recover() != nil {
			a = fallbackElementAnchor(el)
		}
	}()
	res, err := anchor.FromElement(el)
	if err != nil {
		return fallbackElementAnchor(el)
	}
	return res
}

func fallbackElementAnchor(el api.ElementInput) anchor.Anchor {
	return anchor.Anchor{Kind: api.AnchorElement, AID: el.AID, Exact: el.Quote, Approx: true}
}

// injectReviewer injects the reviewer script into an html artifact's
// response stream (it never writes back to the source file). htmlaid could
// likewise still be a panicking skeleton: if injection fails, the content is
// returned unchanged so the page can at least open, just without the
// comment layer for now — an acceptable degradation, since the whole /raw
// entry point must never 500.
func (s *Server) injectReviewer(src []byte) (out []byte, err error) {
	opts := htmlaid.ReviewerOptions{Mode: "review", Disabled: s.opts.Raw}
	defer func() {
		if recover() != nil {
			out, err = src, nil
		}
	}()
	res, ierr := htmlaid.InjectReviewer(src, opts)
	if ierr != nil {
		return src, nil
	}
	return res, nil
}
