package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/six-ddc/art/internal/api"
)

// newCommentsCmd implements art comments [--open|--all] [--doc slug] --json (W-core).
//
// This is the agent's main entry point; the output must match the threads
// field of GET /api/docs/{id}/comments field for field (both []api.Thread),
// including path, line, start/end, quote, prefix/suffix, and context.
// Filters to --open by default; --all outputs every status.
func newCommentsCmd() *cobra.Command {
	var (
		onlyOpen bool
		all      bool
		doc      string
	)
	cmd := &cobra.Command{
		Use:   "comments",
		Short: "List comment threads",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			v, err := openVault()
			if err != nil {
				return err
			}
			c, err := dial(ctx, v)
			if err != nil {
				return err
			}

			status := api.StatusOpen
			if all {
				status = ""
			}
			_ = onlyOpen // documented default; --open is a no-op alias for the default

			var threads []api.Thread
			if c != nil {
				docID := ""
				if doc != "" {
					docs, err := c.Docs(ctx)
					if err != nil {
						return mapNotFound(err)
					}
					d, err := findDocInList(docs.Docs, doc)
					if err != nil {
						return err
					}
					docID = d.ID
				}
				threads, err = c.Comments(ctx, docID, status)
				if err != nil {
					return mapNotFound(err)
				}
			} else {
				if doc != "" {
					art, err := v.Lookup(doc)
					if err != nil {
						return err
					}
					resp, err := v.Threads(ctx, art)
					if err != nil {
						return err
					}
					threads = filterByStatus(resp.Threads, status)
				} else {
					threads, err = v.AllThreads(ctx, status)
					if err != nil {
						return err
					}
				}
			}

			// Belt-and-suspenders: every branch above already returns a
			// non-nil slice, but never let a bare []api.Thread reach --json
			// as a literal `null` (breaks the frontend's non-nullable array
			// contract) regardless of how a future branch might change that.
			if threads == nil {
				threads = []api.Thread{}
			}

			return emit(threads, func() {
				for _, th := range threads {
					fmt.Printf("%s\t%s\t%s\t%s\n", th.Thread, th.Status, th.Path, th.Body)
				}
			})
		},
	}
	cmd.Flags().BoolVar(&onlyOpen, "open", false, "list only open threads (default)")
	cmd.Flags().BoolVar(&all, "all", false, "list threads of every status")
	cmd.Flags().StringVar(&doc, "doc", "", "list threads for only the given artifact")
	return cmd
}
