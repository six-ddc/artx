package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/eventlog"
	"github.com/six-ddc/art/internal/gitx"
)

// newCompactCmd implements art compact [--doc slug] (W-core, M2).
//
// Compaction is the only operation allowed to rewrite existing event blocks,
// so it must hold an exclusive flock, and it produces its own git commit on
// completion: "art: compact <docid>".
func newCompactCmd() *cobra.Command {
	var doc string
	var force bool
	cmd := &cobra.Command{
		Use:   "compact",
		Short: "Compact the comment event log (archive resolved threads, collapse edit/remap chains)",
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

			resp := api.CompactResponse{Stats: []api.CompactStat{}} // never emit `null` for a zero-doc vault
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
				out, err := c.Compact(ctx, api.CompactRequest{Doc: docID, Force: force})
				if err != nil {
					return mapNotFound(err)
				}
				resp = *out
			} else {
				var docIDs []string
				if doc != "" {
					art, err := v.Lookup(doc)
					if err != nil {
						return err
					}
					docIDs = []string{art.ID}
				} else {
					ids, err := v.Store.DocIDs()
					if err != nil {
						return err
					}
					docIDs = ids
				}

				for _, id := range docIDs {
					stat, err := v.Store.Compact(id, eventlog.CompactOptions{Force: force})
					if err != nil {
						return err
					}
					resp.Stats = append(resp.Stats, stat)
					if !stat.Skipped && v.Git != nil {
						if sha, cerr := v.Git.Commit(ctx, gitx.CommitOptions{
							Message: fmt.Sprintf("art: compact %s", id),
							Author:  gitx.AuthorArt,
						}); cerr == nil && sha != "" {
							resp.Commit = sha
						}
					}
				}
			}
			if resp.Stats == nil {
				resp.Stats = []api.CompactStat{}
			}

			return emit(resp, func() {
				for _, s := range resp.Stats {
					fmt.Printf("%s: %d -> %d events, %d threads archived (skipped=%v)\n",
						s.Doc, s.EventsBefore, s.EventsAfter, s.ThreadsArchived, s.Skipped)
				}
			})
		},
	}
	cmd.Flags().StringVar(&doc, "doc", "", "compact only the given artifact")
	cmd.Flags().BoolVar(&force, "force", false, "ignore thresholds and force compaction")
	return cmd
}
