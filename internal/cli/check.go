package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/spf13/cobra"

	"github.com/sunprema/nexus-cli/internal/gitrepo"
	"github.com/sunprema/nexus-cli/internal/paths"
)

// nexusDesyncMarker is the fixed, greppable line prefix the 'narrate'
// skill's Verifier step writes into an explainer file when it finds the
// narrative disagrees with the code — always as the second line of a
// GitHub warning callout: "> [!WARNING]\n> **Nexus desync** — ...". See
// docs/adr/0002-nonblocking-desync-markers.md — main always wins, so this
// marks the disagreement instead of blocking anything.
//
// Matching requires a line to START WITH this prefix (see
// findDesyncedFiles), not merely contain it anywhere. A substring match is
// not enough: an explainer file describing or quoting the marker pattern
// mid-sentence — this file's own entry does, to explain what nexus check
// looks for — contains the exact text without being an actual callout
// line, and would otherwise flag itself as desynced with nothing actually
// wrong. A real marker is always its own line; prose that merely mentions
// the pattern is not.
const nexusDesyncMarker = "> **Nexus desync**"

func newNexusCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Report unresolved desyncs between code and the explainer branch",
		Long: `Scan the 'explainer' branch for unresolved desync markers.

The 'narrate' skill's Verifier step writes a marker into an explainer file
when it finds the narrative disagrees with the code, instead of blocking
the commit (main is always the source of truth — see
docs/adr/0002-nonblocking-desync-markers.md). This command is the read-only
report of what's still marked; it does not itself run any LLM check, and it
does not clear markers — narrating the affected file again is what does
that.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runNexusCheck(cmd)
		},
	}
}

func runNexusCheck(cmd *cobra.Command) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		cmd.SilenceUsage = true
		fmt.Fprintln(cmd.ErrOrStderr(), "Not a git repository. Run 'nexus check' from inside a git repository.")
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

	tree, explainerBranch, err := resolveExplainerTree(repo, repoRoot)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			fmt.Fprintf(out, "Branch %q not found. Run 'nexus init' first.\n", explainerBranch)
			return nil
		}
		return err
	}

	desynced, err := findDesyncedFiles(tree)
	if err != nil {
		return fmt.Errorf("scan %s for desync markers: %w", explainerBranch, err)
	}

	if len(desynced) == 0 {
		fmt.Fprintf(out, "✓ No unresolved desyncs found in %q.\n", explainerBranch)
		return nil
	}

	fmt.Fprintf(out, "%d file(s) with unresolved desyncs in %q:\n\n", len(desynced), explainerBranch)
	for _, d := range desynced {
		fmt.Fprintf(out, "  %s\n", d.path)
		for _, line := range d.lines {
			fmt.Fprintf(out, "    %s\n", line)
		}
	}
	fmt.Fprintln(out, "\nNarrate the affected code file again to re-verify and clear a resolved marker.")
	return nil
}

type nexusDesyncedFile struct {
	path  string
	lines []string
}

// findDesyncedFiles walks tree's Markdown files and collects every one
// currently desynced. A file with frontmatter (see frontmatter.go)
// trusts its `desynced` field, not the marker-line scan — that's the whole
// point of moving the signal there: it can't be fooled by prose that merely
// mentions the marker text (see docs/adr/0004-explainer-frontmatter.md,
// which records two real false-positive bugs the marker-only approach hit
// during dogfooding). Files narrated before frontmatter existed fall back
// to the marker scan, same as always.
func findDesyncedFiles(tree *object.Tree) ([]nexusDesyncedFile, error) {
	var desynced []nexusDesyncedFile
	err := tree.Files().ForEach(func(f *object.File) error {
		if !strings.HasSuffix(f.Name, ".md") {
			return nil
		}
		content, err := f.Contents()
		if err != nil {
			return fmt.Errorf("read %s: %w", f.Name, err)
		}

		lines := nexusDesyncMarkerLines(content)
		isDesynced := len(lines) > 0
		if fm, _, hasFrontmatter := parseNexusFrontmatter(content); hasFrontmatter {
			isDesynced = fm.Desynced
		}
		if !isDesynced {
			return nil
		}
		if len(lines) == 0 {
			// Frontmatter says desynced but there's no inline callout to
			// quote (e.g. a hand-edited frontmatter field) — still report
			// the file, just without a marker line to show.
			lines = []string{"(desynced: true in frontmatter, no inline marker found)"}
		}
		desynced = append(desynced, nexusDesyncedFile{path: f.Name, lines: lines})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return desynced, nil
}

// resolveExplainerBranchRef resolves the explainer branch's name (from
// .nexus/settings.json, defaulting to nexusExplainerBranch) and its current
// ref. Returns plumbing.ErrReferenceNotFound (wrapped) if the branch doesn't
// exist — callers distinguish "not initialized" from other failures with
// errors.Is. This is the one place branch-name resolution happens, so
// 'nexus check', 'nexus show', and 'nexus diff' can't drift from each other.
func resolveExplainerBranchRef(repo *git.Repository, repoRoot string) (ref *plumbing.Reference, branch string, err error) {
	branch = nexusExplainerBranch
	if settings, settingsErr := loadNexusSettings(repoRoot); settingsErr == nil && settings.ExplainerBranch != "" {
		branch = settings.ExplainerBranch
	}

	ref, err = repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		return nil, branch, err
	}
	return ref, branch, nil
}

// resolveExplainerTree opens the explainer branch and returns its tip tree,
// plus the resolved branch name. See resolveExplainerBranchRef for the
// error contract.
func resolveExplainerTree(repo *git.Repository, repoRoot string) (tree *object.Tree, branch string, err error) {
	ref, branch, err := resolveExplainerBranchRef(repo, repoRoot)
	if err != nil {
		return nil, branch, err
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, branch, fmt.Errorf("read %s HEAD commit: %w", branch, err)
	}
	tree, err = commit.Tree()
	if err != nil {
		return nil, branch, fmt.Errorf("read %s tree: %w", branch, err)
	}
	return tree, branch, nil
}

// nexusDesyncMarkerLines returns every line in content that STARTS WITH
// nexusDesyncMarker (after trimming leading whitespace) — i.e. an actual
// callout line, not just the pattern appearing somewhere in the file. See
// nexusDesyncMarker for why Contains is not enough: prose that quotes or
// describes the marker mid-sentence contains the same text without being a
// real marker line. Shared by 'nexus check' (scans every file) and
// 'nexus show' (reports one file's status).
func nexusDesyncMarkerLines(content string) []string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), nexusDesyncMarker) {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines
}
