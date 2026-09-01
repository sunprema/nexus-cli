package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/spf13/cobra"

	"github.com/sunprema/nexus-cli/internal/gitrepo"
	"github.com/sunprema/nexus-cli/internal/paths"
)

// nexusMapEntry is one row in 'nexus map' — either a per-file explainer
// entry or a guided tour, everything a reader can learn from frontmatter
// alone, no LLM call.
type nexusMapEntry struct {
	Path string `json:"path"`
	// Kind is "explainer" for a per-file narrative (Path is a code path)
	// or "tour" for a guided walkthrough (Path is a slug, under
	// nexusTourDir on the explainer branch — see tour.go). Tours
	// don't have SourceCommit/Desynced; use StopCount instead of Summary
	// alone to gauge one at a glance.
	Kind         string `json:"kind"`
	Summary      string `json:"summary,omitempty"`
	SourceCommit string `json:"source_commit,omitempty"`
	Desynced     bool   `json:"desynced"`
	// HasFrontmatter is false for an explainer file narrated before
	// frontmatter existed (docs/adr/0004-explainer-frontmatter.md) —
	// Summary and SourceCommit are empty in that case, and Desynced falls
	// back to the marker-line scan, same as show.go/check.go.
	// Always true for a tour entry (a malformed tour file isn't listed at
	// all — see the walk below).
	HasFrontmatter bool `json:"has_frontmatter"`
	// StopCount is set only for Kind == "tour".
	StopCount int `json:"stop_count,omitempty"`
}

type nexusMapResult struct {
	ExplainerBranch string          `json:"explainer_branch"`
	Count           int             `json:"count"`
	WithSummary     int             `json:"with_summary"`
	Entries         []nexusMapEntry `json:"entries"`
	// HistoryCount is how many history records (history.go) the branch
	// holds. They aren't listed here — they're events, not files or tours,
	// and are queried by path via 'nexus history' / nexus_history — but the
	// count tells a reader whether that lookup is worth making at all.
	HistoryCount int `json:"history_count"`
	// Error mirrors nexusShowResult's: set only for "Nexus isn't set up" /
	// "explainer branch missing", never for the ordinary "zero files
	// narrated yet" case.
	Error string `json:"error,omitempty"`
}

func newNexusMapCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "map",
		Short: "List every narrated file and guided tour, one line each",
		Long: `Walk the 'explainer' branch and print a one-line index: per-file
explainer entries (path, summary, desync status) and guided tours (slug,
title, stop count) — read from YAML frontmatter
(docs/adr/0004-explainer-frontmatter.md), no LLM call, no reading any code.

Meant for getting the gist of an unfamiliar codebase before deciding which
files are worth reading in full, or which tour to start with — the same way
scanning a directory of SKILL.md descriptions beats opening every skill.
Files narrated before frontmatter existed appear with no summary;
re-narrating them backfills it. See 'nexus tour <slug>' for a tour's
full stop list.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runNexusMap(cmd, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output structured JSON instead of plain text")
	return cmd
}

func runNexusMap(cmd *cobra.Command, asJSON bool) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		cmd.SilenceUsage = true
		fmt.Fprintln(cmd.ErrOrStderr(), "Not a git repository. Run 'nexus map' from inside a git repository.")
		return NewSilentError(fmt.Errorf("resolve worktree root: %w", err))
	}

	result, err := computeNexusMap(ctx, repoRoot)
	if err != nil {
		return err
	}

	if asJSON {
		return writeNexusMapJSON(out, result)
	}

	if result.Error != "" {
		fmt.Fprintln(out, result.Error)
		return nil
	}
	if result.Count == 0 {
		fmt.Fprintf(out, "No narrated files found in %q yet.\n", result.ExplainerBranch)
		if result.HistoryCount > 0 {
			fmt.Fprintf(out, "%d history record(s) — see 'nexus history [path]'.\n", result.HistoryCount)
		}
		return nil
	}
	for _, e := range result.Entries {
		if e.Kind == "tour" {
			fmt.Fprintf(out, "  ▶ %s: %s (%d stops)\n", e.Path, e.Summary, e.StopCount)
			continue
		}
		prefix := "  "
		if e.Desynced {
			prefix = "⚠ "
		}
		summary := e.Summary
		if summary == "" {
			summary = "(no summary yet — narrate this file to backfill)"
		}
		fmt.Fprintf(out, "%s%s: %s\n", prefix, e.Path, summary)
	}
	fmt.Fprintf(out, "\n%d file(s) narrated, %d with a summary.\n", result.Count, result.WithSummary)
	if result.HistoryCount > 0 {
		fmt.Fprintf(out, "%d history record(s) — see 'nexus history [path]'.\n", result.HistoryCount)
	}
	return nil
}

// computeNexusMap builds the whole-branch index. Shared by 'nexus map' and
// any future MCP integration. Like computeNexusShow, every "normal"
// outcome — Nexus not set up, branch missing, zero files narrated yet — is
// reported via the result struct, never a Go error.
func computeNexusMap(ctx context.Context, repoRoot string) (nexusMapResult, error) {
	if _, statErr := os.Stat(filepath.Join(repoRoot, nexusSettingsFile)); statErr != nil {
		return nexusMapResult{Error: "Nexus isn't set up in this repo. Run 'nexus init' first."}, nil
	}

	repo, err := gitrepo.OpenCurrent(ctx)
	if err != nil {
		return nexusMapResult{}, fmt.Errorf("open git repository: %w", err)
	}
	defer repo.Close()

	tree, explainerBranch, err := resolveExplainerTree(repo, repoRoot)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nexusMapResult{
				ExplainerBranch: explainerBranch,
				Error:           fmt.Sprintf("Branch %q not found. Run 'nexus init' first.", explainerBranch),
			}, nil
		}
		return nexusMapResult{}, err
	}

	var entries []nexusMapEntry
	historyCount := 0
	err = tree.Files().ForEach(func(f *object.File) error {
		if !strings.HasSuffix(f.Name, ".md") {
			return nil
		}
		content, err := f.Contents()
		if err != nil {
			return fmt.Errorf("read %s: %w", f.Name, err)
		}

		if strings.HasPrefix(f.Name, nexusHistoryDir) {
			// A history record is neither a per-file entry nor a tour;
			// counted, not listed (see HistoryCount). Malformed ones are
			// skipped the same way a stop-less tour is.
			if _, _, ok := parseNexusHistoryFrontmatter(content); ok {
				historyCount++
			}
			return nil
		}

		if strings.HasPrefix(f.Name, nexusTourDir) {
			slug := strings.TrimSuffix(strings.TrimPrefix(f.Name, nexusTourDir), ".md")
			fm, _, ok := parseNexusTourFrontmatter(content)
			if !ok {
				// Malformed or stop-less tour file — not listed, same
				// spirit as a code file with unparseable frontmatter
				// still being listed but without a summary; a tour
				// without stops has nothing to show at all.
				return nil
			}
			entries = append(entries, nexusMapEntry{
				Path:           slug,
				Kind:           "tour",
				Summary:        fm.Title,
				HasFrontmatter: true,
				StopCount:      len(fm.Stops),
			})
			return nil
		}

		// The code path comes from the tree entry's own name, not
		// frontmatter's `path:` field: the tree is ground truth for what a
		// file actually mirrors, while frontmatter is descriptive metadata
		// a narrator/verifier wrote and could in principle drift.
		entry := nexusMapEntry{Path: strings.TrimSuffix(f.Name, ".md"), Kind: "explainer"}
		if fm, _, hasFrontmatter := parseNexusFrontmatter(content); hasFrontmatter {
			entry.Summary = fm.Summary
			entry.SourceCommit = fm.SourceCommit
			entry.Desynced = fm.Desynced
			entry.HasFrontmatter = true
		} else {
			entry.Desynced = len(nexusDesyncMarkerLines(content)) > 0
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nexusMapResult{}, fmt.Errorf("scan %s: %w", explainerBranch, err)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	withSummary := 0
	for _, e := range entries {
		if e.Summary != "" {
			withSummary++
		}
	}

	return nexusMapResult{
		ExplainerBranch: explainerBranch,
		Count:           len(entries),
		WithSummary:     withSummary,
		Entries:         entries,
		HistoryCount:    historyCount,
	}, nil
}

func writeNexusMapJSON(w io.Writer, result nexusMapResult) error {
	enc := json.NewEncoder(w)
	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}
