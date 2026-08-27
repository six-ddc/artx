package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/six-ddc/art/internal/api"
)

// newListCmd implements art list [--json] (W-core).
// --json outputs an api.DocsResponse; human mode prints an aligned table:
// id / slug / type / open / title.
func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all artifacts and their open comment counts",
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

			var resp api.DocsResponse
			if c != nil {
				out, err := c.Docs(ctx)
				if err != nil {
					return mapNotFound(err)
				}
				resp = *out
			} else {
				docs, err := v.Docs(ctx, localBaseURL(v))
				if err != nil {
					return err
				}
				resp = api.DocsResponse{Vault: v.Name, Root: v.Root, Docs: docs}
			}

			return emit(resp, func() {
				w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
				fmt.Fprintln(w, "ID\tSLUG\tTYPE\tOPEN\tTITLE")
				for _, d := range resp.Docs {
					fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", d.ID, d.Slug, d.Type, d.OpenCount, d.Title)
				}
				w.Flush()
			})
		},
	}
}
