package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/spf13/cobra"

	"github.com/sunprema/nexus-cli/internal/gitrepo"
	"github.com/sunprema/nexus-cli/internal/jsonutil"
	"github.com/sunprema/nexus-cli/internal/paths"
)

// nexusDir is the project-level config directory for Nexus.
const nexusDir = ".nexus"

// nexusSettingsFile holds the fixed Nexus configuration for this repo.
const nexusSettingsFile = nexusDir + "/settings.json"

// nexusNarratorPromptFile is the user-editable prompt/skill the (future)
// Narrator Agent uses to turn a code diff into explainer prose. Kept as a
// plain Markdown file rather than embedded in Go so a team can tune tone and
// house style without a CLI release.
const nexusNarratorPromptFile = nexusDir + "/skills/narrator-prompt.md"

// nexusExplainerBranch is the shadow branch that mirrors main's file
// structure with human-readable narrative. Not user-configurable: main is
// always the source of truth, so there is nothing to gain from letting the
// branch name drift from what the rest of Nexus's tooling expects.
const nexusExplainerBranch = "explainer"

// nexusGitignoreFile keeps local, transient Nexus state out of commits to
// main. Only pending.json qualifies today — settings.json and the narrator
// prompt are team-shared config and stay tracked.
const nexusGitignoreFile = nexusDir + "/.gitignore"

const defaultNexusGitignore = "pending.json\n"

// NexusSettings is the .nexus/settings.json schema.
type NexusSettings struct {
	// SourceOfTruth is always "main". Recorded rather than made configurable:
	// FR3.3 conflicts are surfaced as markers in the explainer branch for a
	// human (or a future `nexus resolve`) to work through, never resolved by
	// picking a different authority.
	SourceOfTruth string `json:"source_of_truth"`
	// ExplainerBranch is the branch mirroring main with narrative Markdown.
	ExplainerBranch string `json:"explainer_branch"`
	// VerifierModel names the model the 'narrate' skill should use for the
	// independent verifier subagent it spawns in its step 7 (see
	// docs/adr/0002-nonblocking-desync-markers.md). Empty means "use the
	// coding agent's default subagent model" — this is unset by default so
	// existing repos are unaffected; a team fills it in to pin verification
	// to a specific model (e.g. one it trusts more for catching mismatches,
	// independent of whatever narrated the draft).
	VerifierModel string `json:"verifier_model"`
}

const defaultNarratorPrompt = `# Nexus Narrator Prompt

This is the prompt the Narrator Agent uses to turn a code change into an
explainer entry. Edit it to fit your team's voice — Nexus reads this file
fresh on every run, so changes take effect immediately with no rebuild.

## Instructions

You are writing for a human reviewer who will read this instead of the code
diff. Given a git diff and the surrounding file context:

- Explain *why* the change was made and *how* the logic now flows — not what
  each line does. The reader can already see the code if they want that.
- Keep it short and crisp. A few sentences beats a wall of text.
- Avoid jargon. Prefer plain language a non-specialist teammate can follow.
- Call out edge cases or tricky behavior the code handles.
- For complex control flow, add a small Mermaid diagram instead of prose.

## Output

Write the explanation as Markdown, matching the code file's path with a
'.md' extension in the 'explainer' branch.
`

func newNexusInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up the explainer branch and Nexus config",
		Long: `Set up Project Nexus in this repository.

Creates:
  - The 'explainer' branch (an orphan branch; your current checkout and
    working tree are left untouched).
  - .nexus/settings.json, recording the fixed Nexus configuration.
  - .nexus/skills/narrator-prompt.md, the editable prompt the Narrator Agent
    will use to write explainer entries.
  - .nexusignore, a gitignore-syntax file naming paths the 'narrate' skill
    should skip. Ships with lockfiles and common test-file patterns
    pre-enabled; edit or delete lines to change what gets narrated.
  - A post-commit git hook that queues each commit for explainer narration
    in .nexus/pending.json (see 'nexus sync'). The hook never calls
    an LLM itself — narration happens later, via the 'narrate' skill.

Safe to run more than once: existing pieces are left as-is and reported.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runNexusInit(cmd)
		},
	}
	return cmd
}

func runNexusInit(cmd *cobra.Command) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		cmd.SilenceUsage = true
		fmt.Fprintln(cmd.ErrOrStderr(), "Not a git repository. Run 'nexus init' from inside a git repository.")
		return NewSilentError(fmt.Errorf("resolve worktree root: %w", err))
	}

	repo, err := gitrepo.OpenCurrent(ctx)
	if err != nil {
		return fmt.Errorf("open git repository: %w", err)
	}
	defer repo.Close()

	branchCreated, err := ensureExplainerBranch(ctx, repo)
	if err != nil {
		return fmt.Errorf("set up explainer branch: %w", err)
	}
	if branchCreated {
		fmt.Fprintf(out, "✓ Created orphan branch '%s'\n", nexusExplainerBranch)
	} else {
		fmt.Fprintf(out, "✓ Branch '%s' already exists\n", nexusExplainerBranch)
	}

	settingsCreated, err := ensureNexusSettings(repoRoot)
	if err != nil {
		return fmt.Errorf("write %s: %w", nexusSettingsFile, err)
	}
	if settingsCreated {
		fmt.Fprintf(out, "✓ Wrote %s\n", nexusSettingsFile)
	} else {
		fmt.Fprintf(out, "✓ %s already exists\n", nexusSettingsFile)
	}

	promptCreated, err := ensureNarratorPrompt(repoRoot)
	if err != nil {
		return fmt.Errorf("write %s: %w", nexusNarratorPromptFile, err)
	}
	if promptCreated {
		fmt.Fprintf(out, "✓ Wrote %s\n", nexusNarratorPromptFile)
	} else {
		fmt.Fprintf(out, "✓ %s already exists (left as-is)\n", nexusNarratorPromptFile)
	}

	gitignoreCreated, err := ensureNexusGitignore(repoRoot)
	if err != nil {
		return fmt.Errorf("write %s: %w", nexusGitignoreFile, err)
	}
	if gitignoreCreated {
		fmt.Fprintf(out, "✓ Wrote %s\n", nexusGitignoreFile)
	}

	nexusIgnoreCreated, err := ensureNexusIgnore(repoRoot)
	if err != nil {
		return fmt.Errorf("write %s: %w", nexusIgnoreFile, err)
	}
	if nexusIgnoreCreated {
		fmt.Fprintf(out, "✓ Wrote %s\n", nexusIgnoreFile)
	} else {
		fmt.Fprintf(out, "✓ %s already exists (left as-is)\n", nexusIgnoreFile)
	}

	hooksDir, err := gitHooksDir(ctx)
	if err != nil {
		return fmt.Errorf("resolve git hooks directory: %w", err)
	}
	hookInstalled, hookWarning, err := ensureNexusPostCommitHook(hooksDir)
	if err != nil {
		return fmt.Errorf("install post-commit hook: %w", err)
	}
	switch {
	case hookInstalled:
		fmt.Fprintln(out, "✓ Added Nexus to the post-commit git hook")
	case hookWarning != "":
		fmt.Fprintln(out, hookWarning)
	default:
		fmt.Fprintln(out, "✓ Post-commit hook already has Nexus installed")
	}

	if settingsCreated || promptCreated || gitignoreCreated || nexusIgnoreCreated {
		fmt.Fprintf(out, "\nReview and commit %s and %s to main when ready.\n", nexusDir, nexusIgnoreFile)
	}

	return nil
}

// loadNexusSettings reads .nexus/settings.json. Callers that only need
// ExplainerBranch should treat a read error as "use nexusExplainerBranch" —
// this is best-effort config, not something worth failing a git hook over.
func loadNexusSettings(repoRoot string) (NexusSettings, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, nexusSettingsFile)) //nolint:gosec // repoRoot + fixed suffix
	if err != nil {
		return NexusSettings{}, err
	}
	var s NexusSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return NexusSettings{}, fmt.Errorf("parse %s: %w", nexusSettingsFile, err)
	}
	return s, nil
}

func ensureNexusGitignore(repoRoot string) (created bool, err error) {
	path := filepath.Join(repoRoot, nexusGitignoreFile)
	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return false, statErr
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return false, fmt.Errorf("create %s: %w", nexusDir, err)
	}
	if err := jsonutil.WriteFileAtomic(path, []byte(defaultNexusGitignore), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// ensureExplainerBranch creates the 'explainer' orphan branch if it doesn't
// exist yet. It writes only the branch ref and its commit/tree objects —
// never checks anything out — so the developer's current branch and working
// tree are undisturbed (the branch is meant to be written by a background
// agent via a separate worktree, not the user's active one).
func ensureExplainerBranch(ctx context.Context, repo *git.Repository) (created bool, err error) {
	refName := plumbing.NewBranchReferenceName(nexusExplainerBranch)
	if _, err := repo.Reference(refName, true); err == nil {
		return false, nil
	} else if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return false, fmt.Errorf("check for existing branch: %w", err)
	}

	emptyTree := &object.Tree{}
	treeObj := repo.Storer.NewEncodedObject()
	if err := emptyTree.Encode(treeObj); err != nil {
		return false, fmt.Errorf("encode empty tree: %w", err)
	}
	treeHash, err := repo.Storer.SetEncodedObject(treeObj)
	if err != nil {
		return false, fmt.Errorf("store empty tree: %w", err)
	}

	name, email := gitAuthorFromRepo(repo)
	sig := object.Signature{Name: name, Email: email, When: time.Now()}
	commit := &object.Commit{
		TreeHash:  treeHash,
		Author:    sig,
		Committer: sig,
		Message: "Initialize explainer branch\n\n" +
			"This branch narrates the intent behind changes on main. See " +
			nexusNarratorPromptFile + " for how entries are written.\n",
	}
	commitObj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(commitObj); err != nil {
		return false, fmt.Errorf("encode orphan commit: %w", err)
	}
	commitHash, err := repo.Storer.SetEncodedObject(commitObj)
	if err != nil {
		return false, fmt.Errorf("store orphan commit: %w", err)
	}

	if err := repo.Storer.SetReference(plumbing.NewHashReference(refName, commitHash)); err != nil {
		return false, fmt.Errorf("set branch ref: %w", err)
	}
	return true, nil
}

func ensureNexusSettings(repoRoot string) (created bool, err error) {
	path := filepath.Join(repoRoot, nexusSettingsFile)
	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return false, statErr
	}

	settings := NexusSettings{
		SourceOfTruth:   "main",
		ExplainerBranch: nexusExplainerBranch,
	}
	data, err := jsonutil.MarshalIndentWithNewline(settings, "", "  ")
	if err != nil {
		return false, fmt.Errorf("marshal settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return false, fmt.Errorf("create %s: %w", nexusDir, err)
	}
	if err := jsonutil.WriteFileAtomic(path, data, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func ensureNarratorPrompt(repoRoot string) (created bool, err error) {
	path := filepath.Join(repoRoot, nexusNarratorPromptFile)
	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return false, statErr
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return false, fmt.Errorf("create %s: %w", filepath.Dir(nexusNarratorPromptFile), err)
	}
	if err := jsonutil.WriteFileAtomic(path, []byte(defaultNarratorPrompt), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
