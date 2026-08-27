package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/eventlog"
)

// newReopenCmd implements art reopen <thread> (W-core).
func newReopenCmd() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "reopen <thread>",
		Short: "Reopen a thread",
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

			var resp api.EventResponse
			if c != nil {
				docID, th, err := c.FindThread(ctx, threadRef)
				if err != nil {
					return mapNotFound(err)
				}
				out, err := c.PostEvent(ctx, docID, api.EventRequest{Type: "reopen", Thread: th.Thread, Note: note})
				if err != nil {
					return mapNotFound(err)
				}
				resp = *out
			} else {
				art, th, err := v.FindThread(ctx, threadRef)
				if err != nil {
					return err
				}
				ev := eventlog.NewEvent(eventlog.KindReopen)
				ev.Thread = th.Thread
				ev.By = v.Author()
				ev.Note = note
				if err := v.Store.Append(art.ID, ev); err != nil {
					return err
				}
				resp = api.EventResponse{OK: "ok", Thread: th.Thread, EventID: ev.EID, Status: api.StatusOpen}
			}

			return emit(resp, func() {
				fmt.Println(resp.Thread)
			})
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "reason for reopening")
	return cmd
}
