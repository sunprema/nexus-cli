package cli

import (
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/sunprema/nexus-cli/internal/gitrepo"
	"github.com/sunprema/nexus-cli/internal/paths"
)

// newNexusPostCommitHookCmd is the hidden entrypoint the installed
// post-commit git hook calls (see ensureNexusPostCommitHook). Not for direct
// use — it only ever appends to .nexus/pending.json and must never fail the
// surrounding commit, so every error path here is a silent no-op rather than
// a returned error.
func newNexusPostCommitHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__post-commit-hook",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runNexusPostCommitHook(cmd)
			return nil
		},
	}
}

func runNexusPostCommitHook(cmd *cobra.Command) {
	ctx := cmd.Context()

	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		return
	}

	// Nexus not initialized here (or this checkout predates it): nothing to
	// queue.
	if _, statErr := os.Stat(filepath.Join(repoRoot, nexusSettingsFile)); statErr != nil {
		return
	}

	explainerBranch := nexusExplainerBranch
	if settings, err := loadNexusSettings(repoRoot); err == nil && settings.ExplainerBranch != "" {
		explainerBranch = settings.ExplainerBranch
	}

	repo, err := gitrepo.OpenCurrent(ctx)
	if err != nil {
		return
	}
	defer repo.Close()

	head, err := repo.Head()
	if err != nil {
		return
	}

	// This fires in the explainer worktree too (git hooks are shared across
	// worktrees of the same repo) whenever the 'narrate' skill commits its
	// own narration. Queuing that commit would have Nexus narrate its own
	// narration commits forever.
	if head.Name().IsBranch() && head.Name().Short() == explainerBranch {
		return
	}

	_ = appendNexusPending(repoRoot, head.Hash().String(), time.Now())
}
