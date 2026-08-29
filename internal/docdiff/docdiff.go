// Package docdiff computes version-to-version diffs of vault documents for
// GET /api/docs/{id}/diff.
//
// Three pure functions, one per view:
//   - BlockOps: md top-level block ops (unchanged/modified/added/removed)
//     driving the time-machine rendered view.
//   - ElementOps: html element ops keyed by the stable data-aid, driving the
//     overlay-highlight view.
//   - Unified: line-mode hunks over the raw source, driving the source view
//     for both document types.
//
// All diffing goes through remap.NewDMP so the 1s DiffMain timeout and match
// parameters stay identical to the anchor-remap pipeline; on timeout dmp
// degrades to a coarse delete-all/insert-all diff, which these functions
// absorb as whole-block ops rather than surfacing an error.
package docdiff

import (
	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/six-ddc/artx/internal/remap"
)

// newDMP is the package's single DMP construction point (same rationale as
// remap.NewDMP: diff behavior must not drift between call sites).
func newDMP() *diffmatchpatch.DiffMatchPatch {
	return remap.NewDMP(remap.DefaultOptions())
}
