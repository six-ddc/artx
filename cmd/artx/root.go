package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/client"
	"github.com/six-ddc/artx/internal/config"
	"github.com/six-ddc/artx/internal/vault"
	"github.com/six-ddc/artx/internal/version"
)

// globalFlags holds the persistent flags shared by every command.
type globalFlags struct {
	Vault string // --vault, a vault name or path
	JSON  bool   // --json
}

var gflags globalFlags

// NewRootCmd assembles the full command tree.
//
// Command ownership: serve belongs to W-serve; everything else belongs to
// W-core.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "artx",
		Short:         "Vault and comment loop for agent-produced artifacts",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&gflags.Vault, "vault", "", "vault name or path (inferred from cwd and the registry by default)")
	root.PersistentFlags().BoolVar(&gflags.JSON, "json", false, "output JSON, for agent consumption")

	root.AddCommand(
		newInitCmd(),
		newNewCmd(),
		newPathCmd(),
		newListCmd(),
		newOpenCmd(),
		newServeCmd(),
		newCommentsCmd(),
		newReplyCmd(),
		newAddressedCmd(),
		newResolveCmd(),
		newReopenCmd(),
		newCompactCmd(),
		newDoctorCmd(),
		newVaultCmd(),
		newWatchCmd(),
	)
	return root
}

// openVault is the common entry point every command uses to locate a vault.
func openVault() (*vault.Vault, error) {
	return vault.Discover(gflags.Vault)
}

// dial returns a client for a running serve instance; if none is running it
// returns (nil, nil).
//
// The skeleton of every write command is:
//
//	v, err := openVault()
//	c, _ := dial(ctx, v)
//	if c != nil { ...go through the API... } else { ...hold the flock and write directly... }
func dial(ctx context.Context, v *vault.Vault) (*client.Client, error) {
	if v == nil {
		return nil, nil
	}
	return client.Detect(ctx, v.Root)
}

// emit chooses the output format based on --json: prints v as JSON, or
// otherwise calls human to render it.
func emit(v any, human func()) error {
	if gflags.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	if human != nil {
		human()
	}
	return nil
}

// mapNotFound normalizes a not_found error returned by serve into
// vault.ErrNotFound itself (dropping the client's extra Message text). This
// keeps the unmodified exitCodeFor in main.go mapping it to exit code 2, and
// it makes the "serve running" and "no serve" paths produce the exact same
// error text for the same logical failure, instead of a doubled-up message
// like "thread not found: vault: artifact not found".
func mapNotFound(err error) error {
	if err == nil {
		return nil
	}
	var ce *client.Error
	if errors.As(err, &ce) && ce.Response.Error == api.ErrNotFound {
		return vault.ErrNotFound
	}
	return err
}

// localBaseURL builds a URL for --json output from the vault config, for use
// when no serve instance is running.
func localBaseURL(v *vault.Vault) string {
	host := config.DefaultHost
	port := config.DefaultPort
	if v.Cfg != nil {
		if v.Cfg.Host != "" {
			host = v.Cfg.Host
		}
		if v.Cfg.Port != 0 {
			port = v.Cfg.Port
		}
	}
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

// findDocInList looks up a slug or id (including a unique prefix) in a list
// of api.Doc, so commands connected to a running serve can reuse the same
// resolution rules as vault.Lookup.
func findDocInList(docs []api.Doc, ref string) (*api.Doc, error) {
	for i := range docs {
		if docs[i].Slug == ref {
			return &docs[i], nil
		}
	}
	for i := range docs {
		if docs[i].ID == ref {
			return &docs[i], nil
		}
	}
	match := -1
	for i := range docs {
		if docs[i].ID != "" && strings.HasPrefix(docs[i].ID, ref) {
			if match != -1 {
				return nil, vault.ErrNotFound
			}
			match = i
		}
	}
	if match != -1 {
		return &docs[match], nil
	}
	return nil, vault.ErrNotFound
}

// filterByStatus filters a thread list by status; an empty status means no
// filtering.
func filterByStatus(threads []api.Thread, status string) []api.Thread {
	if status == "" {
		return threads
	}
	out := make([]api.Thread, 0, len(threads))
	for _, th := range threads {
		if th.Status == status {
			out = append(out, th)
		}
	}
	return out
}
