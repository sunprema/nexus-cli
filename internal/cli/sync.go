package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/spf13/cobra"

	"github.com/sunprema/nexus-cli/internal/gitrepo"
	"github.com/sunprema/nexus-cli/internal/paths"
)

func newNexusSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Report commits pending explainer narration",
		Long: `Report which commits haven't been narrated to the 'explainer' branch yet.

This does not perform narration itself — that requires an LLM (see the
'narrate' skill). It only reports what the post-commit hook has queued in
.nexus/pending.json, so you know whether 'explainer' is caught up with
'main'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runNexusSync(cmd)
		},
	}
}

func runNexusSync(cmd *cobra.Command) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		cmd.SilenceUsage = true
		fmt.Fprintln(cmd.ErrOrStderr(), "Not a git repository. Run 'nexus sync' from inside a git repository.")
		return NewSilentError(fmt.Errorf("resolve worktree root: %w", err))
	}

	if _, statErr := os.Stat(filepath.Join(repoRoot, nexusSettingsFile)); statErr != nil {
		fmt.Fprintln(out, "Nexus isn't set up in this repo. Run 'nexus init' first.")
		return nil
	}

	q, err := loadNexusPending(repoRoot)
	if err != nil {
		return fmt.Errorf("read pending queue: %w", err)
	}

	if len(q.Pending) == 0 {
		fmt.Fprintln(out, "✓ explainer is up to date — nothing pending narration.")
		return nil
	}

	fmt.Fprintf(out, "%d commit(s) pending explainer narration:\n\n", len(q.Pending))

	repo, repoErr := gitrepo.OpenCurrent(ctx)
	if repoErr == nil {
		defer repo.Close()
	}

	for _, e := range q.Pending {
		short := e.Commit
		if len(short) > 7 {
			short = short[:7]
		}
		age := formatPendingAge(time.Since(e.RecordedAt))

		subject := ""
		if repoErr == nil {
			if c, commitErr := repo.CommitObject(plumbing.NewHash(e.Commit)); commitErr == nil {
				subject, _, _ = strings.Cut(c.Message, "\n")
			}
		}

		if subject != "" {
			fmt.Fprintf(out, "  %s  %s  (pending %s)\n", short, subject, age)
		} else {
			fmt.Fprintf(out, "  %s  (pending %s; commit not found locally)\n", short, age)
		}
	}

	fmt.Fprintln(out, "\nRun the 'narrate' skill in your coding agent to catch up.")
	return nil
}

func formatPendingAge(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	return d.Round(time.Minute).String()
}
