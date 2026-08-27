package main

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/six-ddc/artx/internal/config"
	"github.com/six-ddc/artx/internal/vault"
)

// newVaultCmd implements artx vault add/list/use (W-core; multi-vault
// registry reserved for M2).
//
// Operates on the global registry ~/.config/artx/config.yaml, the same table
// used internally by config.Register when artx init calls it.
func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Manage the global vault registry",
	}
	cmd.AddCommand(newVaultAddCmd(), newVaultListCmd(), newVaultUseCmd())
	return cmd
}

func newVaultAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <name> <dir>",
		Short: "Register an existing vault directory in the global table",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, dir := args[0], args[1]
			abs, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if _, err := vault.Open(abs, name); err != nil {
				return err
			}
			if err := config.Register(name, abs); err != nil {
				return err
			}
			return emit(map[string]string{"name": name, "root": abs}, func() {
				fmt.Printf("registered %q -> %s\n", name, abs)
			})
		},
	}
}

func newVaultListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered vaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			g, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			names := make([]string, 0, len(g.Vaults))
			for n := range g.Vaults {
				names = append(names, n)
			}
			sort.Strings(names)

			type row struct {
				Name    string `json:"name"`
				Root    string `json:"root"`
				Default bool   `json:"default"`
			}
			rows := make([]row, 0, len(names))
			for _, n := range names {
				rows = append(rows, row{Name: n, Root: g.Vaults[n], Default: n == g.DefaultVault})
			}

			return emit(rows, func() {
				for _, r := range rows {
					marker := " "
					if r.Default {
						marker = "*"
					}
					fmt.Printf("%s %s\t%s\n", marker, r.Name, r.Root)
				}
			})
		},
	}
}

func newVaultUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the default vault",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			g, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			if _, ok := g.Vaults[name]; !ok {
				return config.ErrVaultNotRegistered
			}
			g.DefaultVault = name
			if err := config.SaveGlobal(g); err != nil {
				return err
			}
			return emit(map[string]string{"default_vault": name}, func() {
				fmt.Printf("default vault set to %q\n", name)
			})
		},
	}
}
