package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/six-ddc/art/internal/gitx"
	"github.com/six-ddc/art/internal/lockfile"
)

// doctorIssue is one health-check finding, used for --json output.
type doctorIssue struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
	Fixed  bool   `json:"fixed"`
}

// newDoctorCmd implements art doctor (W-core).
//
// Checks for, and (with --fix) repairs:
//   - .gitattributes missing the merge=union rule
//   - an artifact missing an aid (frontmatter / <meta>)
//   - a corrupt tail in an event log (eventlog.ReadReport.TailCorrupt -> Truncate)
//   - a stale .art/serve.lock
//   - an event log that references a document that no longer exists
func newDoctorCmd() *cobra.Command {
	var fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check a vault's health and fix what can be auto-fixed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}

			issues := []doctorIssue{} // never emit `null` for a clean vault's --json output

			// .gitattributes missing merge=union rule.
			gaPath := filepath.Join(v.Root, ".gitattributes")
			hasRule := false
			if b, rerr := os.ReadFile(gaPath); rerr == nil {
				hasRule = strings.Contains(string(b), gitx.GitattributesLine)
			}
			if !hasRule {
				it := doctorIssue{Kind: "gitattributes", Detail: "missing merge=union rule"}
				if fix {
					if err := gitx.EnsureGitattributes(v.Root); err == nil {
						it.Fixed = true
					}
				}
				issues = append(issues, it)
			}

			arts, err := v.Scan()
			if err != nil {
				return err
			}
			knownIDs := map[string]bool{}
			for _, a := range arts {
				if a.ID == "" {
					issues = append(issues, doctorIssue{Kind: "missing-aid", Detail: a.Slug})
					continue
				}
				knownIDs[a.ID] = true
			}

			// Corrupt tails in event logs.
			docIDs, err := v.Store.DocIDs()
			if err != nil {
				return err
			}
			for _, id := range docIDs {
				_, report, rerr := v.Store.Read(id)
				if rerr != nil {
					return rerr
				}
				if report.TailCorrupt {
					it := doctorIssue{Kind: "corrupt-tail", Detail: id}
					if fix {
						if terr := v.Store.Truncate(id, report.Events); terr == nil {
							it.Fixed = true
						}
					}
					issues = append(issues, it)
				}
				if !knownIDs[id] {
					issues = append(issues, doctorIssue{Kind: "orphaned-log", Detail: id})
				}
			}

			// Stale serve.lock: Probe itself removes a stale lockfile as a
			// side effect (lockfile.Probe's documented self-healing
			// behavior), so we only need to notice it happened.
			lockPath := filepath.Join(v.Root, lockfile.ServeLockName)
			if _, statErr := os.Stat(lockPath); statErr == nil {
				if _, perr := lockfile.Probe(v.Root); errors.Is(perr, lockfile.ErrNoServe) {
					issues = append(issues, doctorIssue{Kind: "stale-serve-lock", Detail: lockPath, Fixed: true})
				}
			}

			if err := emit(issues, func() {
				if len(issues) == 0 {
					fmt.Println("ok: no issues found")
					return
				}
				for _, it := range issues {
					status := "found"
					if it.Fixed {
						status = "fixed"
					}
					fmt.Printf("[%s] %s: %s\n", status, it.Kind, it.Detail)
				}
			}); err != nil {
				return err
			}

			// Exit code must reflect vault health so agents/CI can act on
			// it without re-parsing output: any issue still unresolved
			// after this run (found without --fix, or found but --fix
			// could not resolve it) means a nonzero exit; a clean vault,
			// or one where --fix resolved everything, exits 0.
			unresolved := 0
			for _, it := range issues {
				if !it.Fixed {
					unresolved++
				}
			}
			if unresolved > 0 {
				return fmt.Errorf("art doctor: %d issue(s) unresolved (run with --fix)", unresolved)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "actually perform fixes instead of only reporting")
	return cmd
}
