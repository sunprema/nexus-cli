package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sunprema/nexus-cli/internal/paths"
)

func newNexusNarratedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "narrated <commit>",
		Short: "Mark a commit as narrated, removing it from the pending queue",
		Long: `Remove a commit from .nexus/pending.json.

This is what the 'narrate' skill calls after it writes and commits that
commit's explainer entry to the 'explainer' branch. It only edits the
pending queue; it never touches git history.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNexusNarrated(cmd, args[0])
		},
	}
}

func runNexusNarrated(cmd *cobra.Command, commitish string) error {
	ctx := cmd.Context()

	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		cmd.SilenceUsage = true
		fmt.Fprintln(cmd.ErrOrStderr(), "Not a git repository.")
		return NewSilentError(fmt.Errorf("resolve worktree root: %w", err))
	}

	commit, err := resolveCommitSHA(ctx, repoRoot, commitish)
	if err != nil {
		cmd.SilenceUsage = true
		return NewSilentError(fmt.Errorf("resolve %q: %w", commitish, err))
	}

	found, err := removeNexusPending(repoRoot, commit)
	if err != nil {
		return fmt.Errorf("update pending queue: %w", err)
	}

	short := commit
	if len(short) > 7 {
		short = short[:7]
	}
	if found {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s marked as narrated\n", short)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s wasn't in the pending queue (already narrated, or narrated proactively before the hook queued it)\n", short)
	}
	return nil
}

// resolveCommitSHA expands a short SHA, branch name, or "HEAD" to the full
// commit SHA the pending queue keys on. The queue always stores what
// repo.Head().Hash().String() produced, so lookups must resolve to the same
// form to match reliably.
func resolveCommitSHA(ctx context.Context, repoRoot, commitish string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "--verify", commitish+"^{commit}").Output() //nolint:gosec // G204: fixed "git" binary; args are argv elements, not shell input
	if err != nil {
		return "", fmt.Errorf("not a valid commit: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
