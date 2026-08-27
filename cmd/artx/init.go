package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/six-ddc/artx/internal/vault"
)

// newInitCmd implements artx init [dir] (W-core).
//
// Behavior: creates the .artx/ skeleton, writes .artx/config.yaml, runs git
// init, writes .gitattributes (with the merge=union rule), generates an
// AGENTS.md template, and registers the vault in the global registry. Idempotent
// when the directory is already a vault — it only fills in what's missing.
func newInitCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Create a vault in a directory",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			v, err := vault.Init(cmd.Context(), dir, name)
			if err != nil {
				return err
			}
			return emit(map[string]string{"root": v.Root, "name": v.Name}, func() {
				fmt.Printf("initialized vault %q at %s\n", v.Name, v.Root)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "vault name in the registry (defaults to the directory name)")
	return cmd
}
