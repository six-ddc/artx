package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/six-ddc/artx/internal/api"
	"github.com/six-ddc/artx/internal/client"
	"github.com/six-ddc/artx/internal/eventlog"
	"github.com/six-ddc/artx/internal/lockfile"
	"github.com/six-ddc/artx/internal/vault"
)

// newWatchCmd implements artx watch --dispatch "<cmd>" (W-core; reserved for M2).
//
// When serve is running, it subscribes to serve's SSE /api/stream and
// re-scans for open comments on any event (simpler than filtering precisely
// on comment events, and just as correct: an extra scan is cheap). Otherwise
// it polls the mtime of the .artx/comments directory. When a new open comment
// is found, it runs the dispatch command with sh -c, injecting
// ARTX_THREAD/ARTX_DOC/ARTX_PATH, dispatching each thread at most once per run.
// --once processes a single pass and exits, for testing.
func newWatchCmd() *cobra.Command {
	var dispatch string
	var once bool
	var interval time.Duration
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch for new comments and dispatch a command",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dispatch == "" {
				return fmt.Errorf("artx watch: --dispatch is required")
			}
			ctx := cmd.Context()
			v, err := openVault()
			if err != nil {
				return err
			}
			c, err := dial(ctx, v)
			if err != nil {
				return err
			}

			dispatched := map[string]bool{}
			runPass := func() (int, error) {
				threads, err := fetchOpenThreads(ctx, v, c)
				if err != nil {
					return 0, err
				}
				n := 0
				for _, th := range threads {
					if dispatched[th.Thread] {
						continue
					}
					dispatched[th.Thread] = true
					if derr := runDispatch(dispatch, th); derr != nil {
						fmt.Fprintf(os.Stderr, "artx watch: dispatch failed for %s: %v\n", th.Thread, derr)
						continue
					}
					n++
				}
				return n, nil
			}

			if once {
				_, err := runPass()
				return err
			}

			info, probeErr := lockfile.Probe(v.Root)
			if probeErr == nil {
				return watchSSE(ctx, info, runPass)
			}
			return watchPoll(ctx, v.Root, interval, runPass)
		},
	}
	cmd.Flags().StringVar(&dispatch, "dispatch", "", "shell command to run when a new open comment appears (required)")
	cmd.Flags().BoolVar(&once, "once", false, "process a single pass and exit, for testing")
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "polling interval when no serve is running")
	return cmd
}

func fetchOpenThreads(ctx context.Context, v *vault.Vault, c *client.Client) ([]api.Thread, error) {
	if c != nil {
		return c.Comments(ctx, "", api.StatusOpen)
	}
	return v.AllThreads(ctx, api.StatusOpen)
}

func runDispatch(dispatch string, th api.Thread) error {
	cmd := exec.Command("sh", "-c", dispatch)
	cmd.Env = append(os.Environ(),
		"ARTX_THREAD="+th.Thread,
		"ARTX_DOC="+th.Doc,
		"ARTX_PATH="+th.Path,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func watchPoll(ctx context.Context, root string, interval time.Duration, runPass func() (int, error)) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	var lastMtime time.Time
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if mtime, err := commentsDirMtime(root); err == nil && mtime.After(lastMtime) {
			lastMtime = mtime
			if _, err := runPass(); err != nil {
				fmt.Fprintf(os.Stderr, "artx watch: %v\n", err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

func commentsDirMtime(root string) (time.Time, error) {
	dir := filepath.Join(root, eventlog.CommentsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}, err
	}
	var latest time.Time
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest, nil
}

func watchSSE(ctx context.Context, info *lockfile.ServeInfo, runPass func() (int, error)) error {
	if _, err := runPass(); err != nil {
		fmt.Fprintf(os.Stderr, "artx watch: %v\n", err)
	}
	hc := &http.Client{}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := streamOnce(ctx, hc, info, runPass); err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "artx watch: stream error: %v; retrying in 2s\n", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func streamOnce(ctx context.Context, hc *http.Client, info *lockfile.ServeInfo, runPass func() (int, error)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.BaseURL()+"/api/stream", nil)
	if err != nil {
		return err
	}
	if info.Token != "" {
		req.Header.Set("Authorization", "Bearer "+info.Token)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event:") {
			// Any SSE event is treated as a wake-up signal to re-scan for
			// new open threads; re-diffing against the dispatched set is
			// cheap and avoids depending on the exact event payload shape.
			if _, err := runPass(); err != nil {
				fmt.Fprintf(os.Stderr, "artx watch: %v\n", err)
			}
		}
	}
	return sc.Err()
}
