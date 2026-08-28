package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/spf13/cobra"

	"github.com/sunprema/nexus-cli/internal/gitrepo"
	"github.com/sunprema/nexus-cli/internal/paths"
)

func newNexusDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <path>",
		Short: "Show what the last narration changed for a file's explainer entry",
		Long: `Diff the 'explainer' branch's current narrative for a code file against
the version it replaced.

<path> is a code file path relative to the repo root (e.g. src/auth.py).
This walks the explainer branch's own history for path + '.md' and prints
a unified diff between its two most recent versions — plain 'git diff'
over content the explainer branch already carries, no LLM call involved.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNexusDiff(cmd, args[0])
		},
	}
	return cmd
}

func runNexusDiff(cmd *cobra.Command, path string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		cmd.SilenceUsage = true
		fmt.Fprintln(cmd.ErrOrStderr(), "Not a git repository. Run 'nexus diff' from inside a git repository.")
		return NewSilentError(fmt.Errorf("resolve worktree root: %w", err))
	}

	if _, statErr := os.Stat(filepath.Join(repoRoot, nexusSettingsFile)); statErr != nil {
		fmt.Fprintln(out, "Nexus isn't set up in this repo. Run 'nexus init' first.")
		return nil
	}

	repo, err := gitrepo.OpenCurrent(ctx)
	if err != nil {
		return fmt.Errorf("open git repository: %w", err)
	}
	defer repo.Close()

	ref, branch, err := resolveExplainerBranchRef(repo, repoRoot)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			fmt.Fprintf(out, "Branch %q not found. Run 'nexus init' first.\n", branch)
			return nil
		}
		return err
	}

	explainerPath := path + ".md"

	commits, err := lastTwoCommitsTouching(repo, ref.Hash(), explainerPath)
	if err != nil {
		return fmt.Errorf("walk %s history for %s: %w", branch, explainerPath, err)
	}

	switch len(commits) {
	case 0:
		fmt.Fprintf(out, "No explainer entry for %s yet. Narrate this commit to create one.\n", path)
		return nil
	case 1:
		fmt.Fprintf(out, "%s has only been narrated once — nothing to diff against yet.\n", path)
		return nil
	}

	newTree, err := commits[0].Tree()
	if err != nil {
		return fmt.Errorf("read tree for %s: %w", commits[0].Hash, err)
	}
	oldTree, err := commits[1].Tree()
	if err != nil {
		return fmt.Errorf("read tree for %s: %w", commits[1].Hash, err)
	}

	changes, err := object.DiffTreeContext(ctx, oldTree, newTree)
	if err != nil {
		return fmt.Errorf("diff %s..%s: %w", commits[1].Hash, commits[0].Hash, err)
	}

	for _, change := range changes {
		if change.To.Name != explainerPath && change.From.Name != explainerPath {
			continue
		}
		patch, patchErr := change.PatchContext(ctx)
		if patchErr != nil {
			return fmt.Errorf("build patch for %s: %w", explainerPath, patchErr)
		}
		fmt.Fprint(out, patch.String())
		return nil
	}

	// The two commits the log walk picked up both touched explainerPath
	// (that's what PathFilter guarantees) but produced no content diff —
	// e.g. a mode-only change. Nothing useful to show either way.
	fmt.Fprintf(out, "No content change in the last two narrations of %s.\n", path)
	return nil
}

// lastTwoCommitsTouching walks the explainer branch's history from ref,
// newest first, and returns up to the two most recent commits whose tree
// diff from their first parent includes path. Equivalent to the first two
// commits of `git log -- path`. A returned slice shorter than two means
// path has fewer than two narrated versions yet.
func lastTwoCommitsTouching(repo *git.Repository, ref plumbing.Hash, path string) ([]*object.Commit, error) {
	iter, err := repo.Log(&git.LogOptions{
		From:       ref,
		Order:      git.LogOrderCommitterTime,
		PathFilter: func(p string) bool { return p == path },
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var commits []*object.Commit
	for len(commits) < 2 {
		commit, iterErr := iter.Next()
		if errors.Is(iterErr, io.EOF) {
			break // Fewer than two narrated versions yet, not an error.
		}
		if iterErr != nil {
			return nil, iterErr
		}
		commits = append(commits, commit)
	}
	return commits, nil
}
