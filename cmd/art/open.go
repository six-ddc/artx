package main

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/six-ddc/art/internal/lockfile"
)

// newOpenCmd implements art open [slug] (W-core).
//
// When no serve is running, it tells the user to start one first rather than
// auto-launching a background process (auto-launching would make "who is the
// sole writer" unpredictable). When serve is running, it uses open/xdg-open
// to open http://host:port/ or /a/{id}.
func newOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open [slug|id]",
		Short: "Open the index page or a specific artifact in a browser",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}

			info, err := lockfile.Probe(v.Root)
			if err != nil {
				return fmt.Errorf("art: no serve running for this vault; start one with `art serve` first")
			}

			target := info.BaseURL() + "/"
			if len(args) > 0 {
				art, err := v.Lookup(args[0])
				if err != nil {
					return err
				}
				target = info.BaseURL() + "/a/" + art.ID
			}

			if err := emit(map[string]string{"url": target}, func() {
				fmt.Println(target)
			}); err != nil {
				return err
			}
			return openBrowser(target)
		},
	}
}

func openBrowser(url string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	return c.Start()
}
