package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/eventlog"
)

// newDeleteCmd implements artx delete <thread> (W-core).
//
// Deletion is a tombstone event, not a physical removal: the append-only
// log keeps every block, and fold hides the thread the moment the
// tombstone lands. Agents racing against a deletion stay safe on both
// sides — a reply/addressed referencing the thread after the tombstone
// folds to nothing, and resolving the thread ref itself fails with
// not_found (exit 2) once the thread is hidden.
func newDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <thread>",
		Short: "Delete a thread (a tombstone; the log keeps the raw events until compact)",
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
				out, err := c.PostEvent(ctx, docID, api.EventRequest{Type: "delete", Thread: th.Thread})
				if err != nil {
					return mapNotFound(err)
				}
				resp = *out
			} else {
				art, th, err := v.FindThread(ctx, threadRef)
				if err != nil {
					return err
				}
				ev := eventlog.NewEvent(eventlog.KindDelete)
				ev.Thread = th.Thread
				ev.By = v.Author()
				if err := v.Store.Append(art.ID, ev); err != nil {
					return err
				}
				resp = api.EventResponse{OK: "ok", Thread: th.Thread, EventID: ev.EID}
			}

			return emit(resp, func() {
				fmt.Println(resp.Thread)
			})
		},
	}
}
