package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newPathCmd implements artx path <slug|id> (W-core).
// Returns vault.ErrNotFound (exit code 2) when not found.
func newPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path <slug|id>",
		Short: "Resolve an artifact's absolute path",
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

			var path string
			if c != nil {
				docs, err := c.Docs(ctx)
				if err != nil {
					return mapNotFound(err)
				}
				d, err := findDocInList(docs.Docs, args[0])
				if err != nil {
					return err
				}
				path = d.Path
			} else {
				art, err := v.Lookup(args[0])
				if err != nil {
					return err
				}
				path = art.Path
			}

			return emit(map[string]string{"path": path}, func() {
				fmt.Println(path)
			})
		},
	}
}
