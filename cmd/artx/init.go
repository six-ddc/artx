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
// The target must be fresh ground (a new or empty directory, outside any
// existing git repository) unless --force is given.
func newInitCmd() *cobra.Command {
	var name string
	var force bool
	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Create a vault in a new or empty directory (defaults to the current one)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			v, err := vault.Init(cmd.Context(), dir, vault.InitOptions{Name: name, Force: force})
			if err != nil {
				return err
			}
			return emit(map[string]string{"root": v.Root, "name": v.Name}, func() {
				fmt.Printf("initialized vault %q at %s\n", v.Name, v.Root)
				fmt.Printf("  .artx/       config, comment threads, assets\n")
				fmt.Printf("  AGENTS.md    the agent protocol — point your coding agent at it\n")
				if v.Git.Available() {
					fmt.Printf("  git          repository ready, skeleton committed\n")
				} else {
					fmt.Printf("  git          not available — versioning and sync are disabled\n")
				}
				fmt.Printf("registered as %q in the global registry\n", v.Name)
				fmt.Printf("next: artx new <slug> --type md|html\n")
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "vault name in the registry (defaults to the directory name)")
	cmd.Flags().BoolVar(&force, "force", false, "allow creating the vault in a non-empty directory or inside an existing git repository")
	return cmd
}
