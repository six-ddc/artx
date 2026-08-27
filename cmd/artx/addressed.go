package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/eventlog"
)

// newAddressedCmd implements artx addressed <thread> [--commit sha] (W-core).
// When --commit is omitted and the vault is under git, it automatically uses
// the current HEAD.
func newAddressedCmd() *cobra.Command {
	var commit, note string
	cmd := &cobra.Command{
		Use:   "addressed <thread>",
		Short: "Mark a thread as addressed (for agents; resolve is for humans)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			threadRef := args[0]

			v, err := openVault()
			if err != nil {
				return err
			}
			c, err := dial(ctx, v)
			if err != nil {
				return err
			}

			if commit == "" && v.Git != nil {
				if sha, gerr := v.Git.HeadSHA(ctx); gerr == nil {
					commit = sha
				}
			}

			var resp api.EventResponse
			if c != nil {
				docID, th, err := c.FindThread(ctx, threadRef)
				if err != nil {
					return mapNotFound(err)
				}
				out, err := c.PostEvent(ctx, docID, api.EventRequest{
					Type: "addressed", Thread: th.Thread, Commit: commit, Note: note,
				})
				if err != nil {
					return mapNotFound(err)
				}
				resp = *out
			} else {
				art, th, err := v.FindThread(ctx, threadRef)
				if err != nil {
					return err
				}
				ev := eventlog.NewEvent(eventlog.KindAddressed)
				ev.Thread = th.Thread
				ev.By = v.Author()
				ev.Commit = commit
				ev.Note = note
				if err := v.Store.Append(art.ID, ev); err != nil {
					return err
				}
				resp = api.EventResponse{OK: "ok", Thread: th.Thread, EventID: ev.EID, Status: api.StatusAddressed}
			}

			return emit(resp, func() {
				fmt.Println(resp.Thread)
			})
		},
	}
	cmd.Flags().StringVar(&commit, "commit", "", "associated commit sha; defaults to HEAD if omitted")
	cmd.Flags().StringVar(&note, "note", "", "additional note")
	return cmd
}
