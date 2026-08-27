package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/six-ddc/artx/internal/eventlog"
	"github.com/six-ddc/artx/internal/server"
	"github.com/six-ddc/artx/internal/vault"
)

// newServeCmd implements artx serve (W-serve).
//
// Security line: if --host points to something other than
// 127.0.0.1/localhost, --token must also be given, or server.New refuses to
// start and returns server.ErrTokenRequired.
func newServeCmd() *cobra.Command {
	var (
		host    string
		port    int
		token   string
		noWatch bool
		open    bool
		raw     bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local HTTP server (reading, comments, SSE, watcher)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}

			if host == "" {
				host = "127.0.0.1"
				if v.Cfg != nil && v.Cfg.Host != "" {
					host = v.Cfg.Host
				}
			}
			if port == 0 {
				port = 7777
				if v.Cfg != nil && v.Cfg.Port != 0 {
					port = v.Cfg.Port
				}
			}
			watch := !noWatch
			if watch && v.Cfg != nil && v.Cfg.Watch != nil {
				watch = *v.Cfg.Watch
			}

			srv, err := server.New(server.Options{
				Vault: v, Host: host, Port: port, Token: token, Watch: watch, Open: open, Raw: raw,
			})
			if err != nil {
				return err
			}

			if server.Placeholder() {
				fmt.Fprintln(os.Stderr, "============================================================")
				fmt.Fprintln(os.Stderr, " Warning: the embedded frontend is a placeholder page (`make web` has not been run to build it yet)")
				fmt.Fprintln(os.Stderr, " The page will appear blank; run `make web` and rebuild the artx binary")
				fmt.Fprintln(os.Stderr, "============================================================")
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if v.Store != nil {
				go compactLoop(ctx, v)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "artx serve listening on %s (vault %s)\n", srv.BaseURL(), v.Root)
			return srv.Run(ctx)
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "listen address, default 127.0.0.1; --token is required for a non-local host")
	cmd.Flags().IntVar(&port, "port", 0, "listen port, defaults to the vault config (7777)")
	cmd.Flags().StringVar(&token, "token", "", "Bearer token, required when --host is non-local")
	cmd.Flags().BoolVar(&noWatch, "no-watch", false, "disable file watching and automatic remapping")
	cmd.Flags().BoolVar(&open, "open", false, "automatically open a browser after starting")
	cmd.Flags().BoolVar(&raw, "raw", false, "escape hatch for html artifacts: /raw skips injecting the reviewer script, giving up the comment layer")
	return cmd
}

// compactCheckInterval is how often serve checks the compact thresholds.
const compactCheckInterval = 10 * time.Minute

// compactLoop periodically checks whether each document has crossed a
// compact threshold (>256KB, or resolved threads older than 30 days) and
// compacts it automatically if so (design doc §6.3).
func compactLoop(ctx context.Context, v *vault.Vault) {
	ticker := time.NewTicker(compactCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCompactSweep(v)
		}
	}
}

func runCompactSweep(v *vault.Vault) {
	ids, err := v.Store.DocIDs()
	if err != nil {
		return
	}
	for _, id := range ids {
		opts := eventlog.CompactOptions{}
		need, err := v.Store.NeedsCompact(id, opts)
		if err != nil || !need {
			continue
		}
		_, _ = v.Store.Compact(id, opts)
	}
}
