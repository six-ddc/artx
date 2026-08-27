package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/six-ddc/artx/internal/api"
)

// newNewCmd implements artx new <slug> --type md|html (W-core).
//
// Outputs api.NewDocResponse: {"id","path","url","slug","type"}. The CLI does
// not do content I/O — once the agent has the path, it writes content with
// its own Read/Edit tools. The port in the URL: use serve's actual port if
// detected, otherwise the vault-configured Port.
func newNewCmd() *cobra.Command {
	var typ, title string
	cmd := &cobra.Command{
		Use:   "new <slug>",
		Short: "Create an artifact skeleton and assign it an id",
		Args:  cobra.ExactArgs(1),
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

			var resp api.NewDocResponse
			if c != nil {
				out, err := c.NewDoc(ctx, api.NewDocRequest{Slug: args[0], Type: typ, Title: title})
				if err != nil {
					return mapNotFound(err)
				}
				resp = *out
			} else {
				art, err := v.New(args[0], typ, title)
				if err != nil {
					return err
				}
				url := strings.TrimRight(localBaseURL(v), "/") + "/a/" + art.ID
				resp = api.NewDocResponse{ID: art.ID, Path: art.Path, URL: url, Slug: art.Slug, Type: art.Type}
			}

			return emit(resp, func() {
				fmt.Printf("%s\t%s\t%s\n", resp.ID, resp.Path, resp.URL)
			})
		},
	}
	cmd.Flags().StringVar(&typ, "type", "md", "artifact type: md or html")
	cmd.Flags().StringVar(&title, "title", "", "title, written into the frontmatter / <title>")
	return cmd
}
