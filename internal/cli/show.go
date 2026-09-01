package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/spf13/cobra"

	"github.com/sunprema/nexus-cli/internal/gitrepo"
	"github.com/sunprema/nexus-cli/internal/paths"
)

// nexusShowResult is the --json shape of 'nexus show'. It is the data
// source IDE integrations (e.g. a VS Code virtual document provider) are
// expected to shell out to, rather than reading the explainer branch's git
// objects themselves — this command is the single place that resolves the
// explainer branch name and the path-mapping convention, so an editor
// extension can't drift from what the CLI actually does.
type nexusShowResult struct {
	Path            string   `json:"path"`
	ExplainerPath   string   `json:"explainer_path"`
	ExplainerBranch string   `json:"explainer_branch"`
	Found           bool     `json:"found"`
	Desynced        bool     `json:"desynced"`
	DesyncMarkers   []string `json:"desync_markers,omitempty"`
	// Summary and SourceCommit come from the file's YAML frontmatter (see
	// frontmatter.go) when present. Empty for a file narrated before
	// frontmatter existed — not an error, just nothing to report yet.
	Summary      string `json:"summary,omitempty"`
	SourceCommit string `json:"source_commit,omitempty"`
	// Tests is set only when this file's frontmatter carries a per-test
	// intent breakdown — i.e. narrate recognized it as a test file. Empty
	// for an ordinary file, not an error.
	Tests   []nexusTestIntent `json:"tests,omitempty"`
	Content string            `json:"content"`
	// History lists the incident/decision/revert records anchored to this
	// path (see history.go) — deliberately kept out of the explainer file's
	// own content so the narrative stays uncluttered, but surfaced here so
	// an agent that calls this before editing a file sees what has gone
	// wrong here before without a second lookup. Populated even when
	// found=false: a file can have history before it has narration.
	History []nexusHistoryEntry `json:"history,omitempty"`
	// Error distinguishes "Nexus isn't set up here at all" / "the explainer
	// branch doesn't exist" from the ordinary found=false case (Nexus is
	// working fine, this particular file just hasn't been narrated yet).
	// Empty in every other case, including the ordinary found=false one —
	// callers should check this before treating found=false as "run
	// narrate", since that isn't the fix when Nexus isn't set up at all.
	Error string `json:"error,omitempty"`
}

func newNexusShowCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "show <path>",
		Short: "Print the current explainer entry for a code file",
		Long: `Print the 'explainer' branch's current narrative for a code file.

<path> is a code file path relative to the repo root (e.g. src/auth.py).
This looks up path + '.md' on the tip of the explainer branch directly —
no checkout, no worktree — and reports whether an entry exists yet and
whether it's flagged with a desync marker.

With --json, also surfaces the file's YAML frontmatter as separate fields
(summary, source_commit) when present — a one-sentence gist without
parsing the full narrative yourself. A test file also carries a 'tests'
list: one entry per test function naming what it actually verifies,
rather than restating its assertions.

This is the data source for IDE integrations: shell out to
'nexus show <path> --json' rather than reading git objects
directly, so path-mapping and branch-name resolution stay in one place.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNexusShow(cmd, args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output structured JSON instead of plain text")
	return cmd
}

func runNexusShow(cmd *cobra.Command, path string, asJSON bool) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		cmd.SilenceUsage = true
		fmt.Fprintln(cmd.ErrOrStderr(), "Not a git repository. Run 'nexus show' from inside a git repository.")
		return NewSilentError(fmt.Errorf("resolve worktree root: %w", err))
	}

	result, err := computeNexusShow(ctx, repoRoot, path)
	if err != nil {
		return err
	}

	if asJSON {
		return writeNexusShowJSON(out, result)
	}
	switch {
	case result.Error != "":
		fmt.Fprintln(out, result.Error)
	case !result.Found:
		fmt.Fprintf(out, "No explainer entry for %s yet. Narrate this commit to create one.\n", path)
	default:
		fmt.Fprint(out, result.Content)
	}
	return nil
}

// computeNexusShow resolves the current explainer status for a code file:
// whether Nexus is set up, whether an entry exists yet, and whether it's
// desync-flagged. Shared by the 'nexus show' CLI command and any future MCP
// integration, so path-mapping and branch-name resolution can't drift
// between surfaces.
//
// Every "normal" outcome — Nexus not set up, explainer branch missing, no
// entry yet, entry found (desynced or not) — is reported via the returned
// nexusShowResult, never a Go error. A returned error means something
// actually went wrong (can't open the repo, can't read a blob), not "no
// explainer to show yet".
func computeNexusShow(ctx context.Context, repoRoot, path string) (nexusShowResult, error) {
	if _, statErr := os.Stat(filepath.Join(repoRoot, nexusSettingsFile)); statErr != nil {
		return nexusShowResult{Path: path, Error: "Nexus isn't set up in this repo. Run 'nexus init' first."}, nil
	}

	repo, err := gitrepo.OpenCurrent(ctx)
	if err != nil {
		return nexusShowResult{}, fmt.Errorf("open git repository: %w", err)
	}
	defer repo.Close()

	tree, explainerBranch, err := resolveExplainerTree(repo, repoRoot)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nexusShowResult{
				Path:            path,
				ExplainerBranch: explainerBranch,
				Error:           fmt.Sprintf("Branch %q not found. Run 'nexus init' first.", explainerBranch),
			}, nil
		}
		return nexusShowResult{}, err
	}

	explainerPath := path + ".md"
	result := nexusShowResult{
		Path:            path,
		ExplainerPath:   explainerPath,
		ExplainerBranch: explainerBranch,
	}

	history, err := collectNexusHistory(tree, path)
	if err != nil {
		return nexusShowResult{}, fmt.Errorf("scan %s for history records: %w", explainerBranch, err)
	}
	result.History = history

	f, err := tree.File(explainerPath)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return result, nil
		}
		return nexusShowResult{}, fmt.Errorf("read %s from %s: %w", explainerPath, explainerBranch, err)
	}

	content, err := f.Contents()
	if err != nil {
		return nexusShowResult{}, fmt.Errorf("read contents of %s: %w", explainerPath, err)
	}

	result.Found = true
	result.Content = content
	result.DesyncMarkers = nexusDesyncMarkerLines(content)

	if fm, _, hasFrontmatter := parseNexusFrontmatter(content); hasFrontmatter {
		result.Summary = fm.Summary
		result.SourceCommit = fm.SourceCommit
		result.Tests = fm.Tests
		// Frontmatter is the authoritative desync signal when present — see
		// docs/adr/0004-explainer-frontmatter.md for why it's more robust
		// than the marker-line scan alone. Fall back to the marker scan
		// only for files narrated before frontmatter existed.
		result.Desynced = fm.Desynced
	} else {
		result.Desynced = len(result.DesyncMarkers) > 0
	}
	return result, nil
}

func writeNexusShowJSON(w io.Writer, result nexusShowResult) error {
	enc := json.NewEncoder(w)
	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}
