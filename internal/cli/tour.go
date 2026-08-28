package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/sunprema/nexus-cli/internal/gitrepo"
	"github.com/sunprema/nexus-cli/internal/paths"
)

// nexusTourDir is the reserved, non-file-mirroring directory on the
// explainer branch that holds guided tours — cross-cutting narrative that
// isn't "about" any single code file, the same way .nexus/settings.json is
// a reserved path on main rather than a real source file. 'nexus map'
// recognizes anything under this prefix as a tour instead of a per-file
// explainer entry.
const nexusTourDir = ".nexus/tours/"

// nexusTourStop is one waypoint in a tour: a file (and optionally a line
// within it) plus the one- or two-sentence reason it's worth stopping here
// before moving on. A stop deliberately does not duplicate that file's own
// explainer narrative — it links to it by path, so the tour stays cheap to
// keep in sync (it's ordering and transitions, not a second copy of the
// per-file content).
type nexusTourStop struct {
	Path string `yaml:"path" json:"path"`
	Line int    `yaml:"line,omitempty" json:"line,omitempty"`
	Note string `yaml:"note" json:"note"`
}

// nexusTourFrontmatter is the YAML frontmatter a tour file carries. Unlike
// nexusExplainerFrontmatter, the frontmatter *is* the tour's substance
// (Stops); the Markdown body below it is optional framing prose (why this
// tour exists, what it covers).
type nexusTourFrontmatter struct {
	Title string          `yaml:"title"`
	Stops []nexusTourStop `yaml:"stops"`
}

// parseNexusTourFrontmatter parses a tour file's frontmatter. ok is false
// when content has no frontmatter block at all, or the block fails to
// parse as YAML, or it parses but declares zero stops — a tour with no
// stops isn't a tour, so callers (map.go's walk in particular) treat
// that the same as "not a tour" rather than showing an empty entry.
func parseNexusTourFrontmatter(content string) (fm nexusTourFrontmatter, body string, ok bool) {
	raw, splitBody, split := splitNexusFrontmatter(content)
	if !split {
		return nexusTourFrontmatter{}, content, false
	}
	if err := yaml.Unmarshal([]byte(raw), &fm); err != nil {
		return nexusTourFrontmatter{}, content, false
	}
	if len(fm.Stops) == 0 {
		return nexusTourFrontmatter{}, content, false
	}
	return fm, splitBody, true
}

// nexusTourResult is the --json shape of 'nexus tour'.
type nexusTourResult struct {
	Slug            string          `json:"slug"`
	ExplainerBranch string          `json:"explainer_branch"`
	Found           bool            `json:"found"`
	Title           string          `json:"title,omitempty"`
	Stops           []nexusTourStop `json:"stops,omitempty"`
	Body            string          `json:"body,omitempty"`
	// Error mirrors nexusShowResult's: set only for "Nexus isn't set up" /
	// "explainer branch missing" / "malformed tour file", never for the
	// ordinary "no tour with this slug" case.
	Error string `json:"error,omitempty"`
}

func newNexusTourCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "tour <slug>",
		Short: "Show a guided tour's stops",
		Long: `Print a guided tour: an ordered list of files worth visiting, each with a
short note on why it matters, meant for onboarding an unfamiliar codebase.

<slug> identifies the tour (e.g. "request-lifecycle"), not a code path —
tours are cross-cutting, not about any single file. See it listed in
'nexus map' alongside per-file explainer entries.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNexusTourShow(cmd, args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output structured JSON instead of plain text")
	return cmd
}

func runNexusTourShow(cmd *cobra.Command, slug string, asJSON bool) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		cmd.SilenceUsage = true
		fmt.Fprintln(cmd.ErrOrStderr(), "Not a git repository. Run 'nexus tour' from inside a git repository.")
		return NewSilentError(fmt.Errorf("resolve worktree root: %w", err))
	}

	result, err := computeNexusTourShow(ctx, repoRoot, slug)
	if err != nil {
		return err
	}

	if asJSON {
		enc := json.NewEncoder(out)
		if err := enc.Encode(result); err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
		return nil
	}

	switch {
	case result.Error != "":
		fmt.Fprintln(out, result.Error)
	case !result.Found:
		fmt.Fprintf(out, "No tour named %q. See 'nexus map' for the tours that exist.\n", slug)
	default:
		fmt.Fprintf(out, "%s\n\n", result.Title)
		if result.Body != "" {
			fmt.Fprintf(out, "%s\n\n", strings.TrimSpace(result.Body))
		}
		for i, stop := range result.Stops {
			if stop.Line > 0 {
				fmt.Fprintf(out, "%d. %s:%d — %s\n", i+1, stop.Path, stop.Line, stop.Note)
			} else {
				fmt.Fprintf(out, "%d. %s — %s\n", i+1, stop.Path, stop.Note)
			}
		}
	}
	return nil
}

// computeNexusTourShow resolves one tour by slug. Shared by 'nexus tour'
// and any future MCP integration, same drift-proofing reason as
// computeNexusShow. Every "normal" outcome — Nexus not set up, branch
// missing, no tour with this slug — is reported via the result, never a Go
// error.
func computeNexusTourShow(ctx context.Context, repoRoot, slug string) (nexusTourResult, error) {
	if _, statErr := os.Stat(filepath.Join(repoRoot, nexusSettingsFile)); statErr != nil {
		return nexusTourResult{Slug: slug, Error: "Nexus isn't set up in this repo. Run 'nexus init' first."}, nil
	}

	repo, err := gitrepo.OpenCurrent(ctx)
	if err != nil {
		return nexusTourResult{}, fmt.Errorf("open git repository: %w", err)
	}
	defer repo.Close()

	tree, explainerBranch, err := resolveExplainerTree(repo, repoRoot)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nexusTourResult{
				Slug:            slug,
				ExplainerBranch: explainerBranch,
				Error:           fmt.Sprintf("Branch %q not found. Run 'nexus init' first.", explainerBranch),
			}, nil
		}
		return nexusTourResult{}, err
	}

	result := nexusTourResult{Slug: slug, ExplainerBranch: explainerBranch}

	tourPath := nexusTourDir + slug + ".md"
	f, err := tree.File(tourPath)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return result, nil
		}
		return nexusTourResult{}, fmt.Errorf("read %s from %s: %w", tourPath, explainerBranch, err)
	}

	content, err := f.Contents()
	if err != nil {
		return nexusTourResult{}, fmt.Errorf("read contents of %s: %w", tourPath, err)
	}

	fm, body, ok := parseNexusTourFrontmatter(content)
	if !ok {
		result.Error = fmt.Sprintf("%s exists but isn't a valid tour (missing or malformed frontmatter, or no stops).", tourPath)
		return result, nil
	}

	result.Found = true
	result.Title = fm.Title
	result.Stops = fm.Stops
	result.Body = body
	return result, nil
}
