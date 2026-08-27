package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/eventlog"
	"github.com/six-ddc/artx/internal/idgen"
)

// newReplyCmd implements artx reply <thread> <text> (W-core).
// When text is "-", it is read from stdin, so an agent can write a
// multi-line explanation.
func newReplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reply <thread> <text>",
		Short: "Append a reply to a thread",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			threadRef, text := args[0], args[1]
			if text == "-" {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				text = string(data)
			}

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
				out, err := c.PostEvent(ctx, docID, api.EventRequest{Type: "reply", Thread: th.Thread, Body: text})
				if err != nil {
					return mapNotFound(err)
				}
				resp = *out
			} else {
				art, th, err := v.FindThread(ctx, threadRef)
				if err != nil {
					return err
				}
				ev := eventlog.NewEvent(eventlog.KindReply)
				ev.Thread = th.Thread
				ev.ID = idgen.ReplyID(th.Thread)
				ev.Author = v.Author()
				ev.Body = text
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
