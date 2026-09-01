package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/sunprema/nexus-cli/internal/gitrepo"
	"github.com/sunprema/nexus-cli/internal/paths"
)

// nexusHistoryDir is the reserved, non-file-mirroring directory on the
// explainer branch that holds history records — short, path-anchored notes
// about events that outlive the diff: a production incident and its fix, a
// decision taken on purpose, a revert. Same idea as nexusTourDir: the
// per-file explainer describes what the code IS now; a history record
// remembers what HAPPENED to it, which the explainer structurally can't
// (after a revert, the explainer looks like nothing ever changed).
// See docs/adr/0006-path-anchored-history-records.md.
const nexusHistoryDir = ".nexus/history/"

// nexusHistoryFrontmatter is the YAML frontmatter a history record carries.
// Like a tour, the frontmatter is most of the substance; the Markdown body
// below it is the one paragraph a reader actually needs.
//
// Every record must be anchored to at least one path — that's the scope
// fence that keeps this from turning into a wiki. A record that can't be
// tied to a path doesn't belong in Nexus.
type nexusHistoryFrontmatter struct {
	// Kind is "incident", "decision", or "revert" (free-form is tolerated;
	// empty defaults to "note").
	Kind  string `yaml:"kind"`
	Title string `yaml:"title"`
	// Date is YYYY-MM-DD — the date of the underlying commit, not of the
	// narration run.
	Date         string   `yaml:"date"`
	SourceCommit string   `yaml:"source_commit,omitempty"`
	Paths        []string `yaml:"paths"`
	// Ref is an external identifier the team already uses (INC-4471,
	// ADR-0004, JIRA-123). A pointer, never the ticket's content.
	Ref string `yaml:"ref,omitempty"`
	// Link is an optional URL to the source of truth (postmortem, ticket).
	Link string `yaml:"link,omitempty"`
}

// nexusHistoryEntry is one record as reported by 'nexus history' (and
// embedded in 'nexus show --json' / nexus_explainer).
type nexusHistoryEntry struct {
	// ID is the record's file name under nexusHistoryDir, minus ".md".
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Title        string   `json:"title"`
	Date         string   `json:"date"`
	SourceCommit string   `json:"source_commit,omitempty"`
	Paths        []string `json:"paths"`
	Ref          string   `json:"ref,omitempty"`
	Link         string   `json:"link,omitempty"`
	// Body is the record's paragraph: what happened, and what someone about
	// to touch these paths should know.
	Body string `json:"body"`
}

// nexusHistoryResult is the --json shape of 'nexus history'.
type nexusHistoryResult struct {
	// Path is the query path, empty when listing every record.
	Path            string              `json:"path,omitempty"`
	ExplainerBranch string              `json:"explainer_branch"`
	Count           int                 `json:"count"`
	Entries         []nexusHistoryEntry `json:"entries"`
	// Error mirrors nexusShowResult's: set only for "Nexus isn't set up" /
	// "explainer branch missing", never for the ordinary "no records" case.
	Error string `json:"error,omitempty"`
}

// parseNexusHistoryFrontmatter parses a history record. ok is false when
// content has no frontmatter block, the block fails to parse, or the
// record has no title or no anchoring path — an unanchored record isn't
// listed anywhere, by design.
func parseNexusHistoryFrontmatter(content string) (fm nexusHistoryFrontmatter, body string, ok bool) {
	raw, splitBody, split := splitNexusFrontmatter(content)
	if !split {
		return nexusHistoryFrontmatter{}, content, false
	}
	if err := yaml.Unmarshal([]byte(raw), &fm); err != nil {
		return nexusHistoryFrontmatter{}, content, false
	}
	if strings.TrimSpace(fm.Title) == "" || len(fm.Paths) == 0 {
		return nexusHistoryFrontmatter{}, content, false
	}
	if fm.Kind == "" {
		fm.Kind = "note"
	}
	return fm, splitBody, true
}

// nexusHistoryPathMatches reports whether a record anchored at anchor is
// relevant to a query path. Containment works both ways: a record anchored
// to a directory applies to every file under it, and asking about a
// directory returns records anchored to files inside it. "." anchors a
// record to the whole repo.
func nexusHistoryPathMatches(query, anchor string) bool {
	q := strings.Trim(pathpkg.Clean(strings.TrimSpace(query)), "/")
	a := strings.Trim(pathpkg.Clean(strings.TrimSpace(anchor)), "/")
	if a == "." || a == "" {
		return true
	}
	if q == "." || q == "" {
		return true
	}
	return q == a || strings.HasPrefix(q, a+"/") || strings.HasPrefix(a, q+"/")
}

// collectNexusHistory walks tree's history records, keeping those relevant
// to query (all of them when query is empty), newest first. Malformed or
// unanchored records are skipped, not errors — same spirit as map.go's
// handling of a stop-less tour.
func collectNexusHistory(tree *object.Tree, query string) ([]nexusHistoryEntry, error) {
	var entries []nexusHistoryEntry
	err := tree.Files().ForEach(func(f *object.File) error {
		if !strings.HasPrefix(f.Name, nexusHistoryDir) || !strings.HasSuffix(f.Name, ".md") {
			return nil
		}
		content, err := f.Contents()
		if err != nil {
			return fmt.Errorf("read %s: %w", f.Name, err)
		}
		fm, body, ok := parseNexusHistoryFrontmatter(content)
		if !ok {
			return nil
		}
		if query != "" {
			relevant := false
			for _, p := range fm.Paths {
				if nexusHistoryPathMatches(query, p) {
					relevant = true
					break
				}
			}
			if !relevant {
				return nil
			}
		}
		entries = append(entries, nexusHistoryEntry{
			ID:           strings.TrimSuffix(strings.TrimPrefix(f.Name, nexusHistoryDir), ".md"),
			Kind:         fm.Kind,
			Title:        fm.Title,
			Date:         fm.Date,
			SourceCommit: fm.SourceCommit,
			Paths:        fm.Paths,
			Ref:          fm.Ref,
			Link:         fm.Link,
			Body:         strings.TrimSpace(body),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortNexusHistory(entries)
	return entries, nil
}

// sortNexusHistory orders newest first: by date, then by id (which the
// narrate skill also prefixes with the date, so ties break sensibly).
func sortNexusHistory(entries []nexusHistoryEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Date != entries[j].Date {
			return entries[i].Date > entries[j].Date
		}
		return entries[i].ID > entries[j].ID
	})
}

func newNexusHistoryCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "history [path]",
		Short: "List incidents, decisions, and reverts recorded against a path",
		Long: `Print the history records on the 'explainer' branch: short, path-anchored
notes about what has happened to the code — a production incident and its
fix, a decision taken on purpose, a revert — that the per-file explainer
can't hold because it only describes the code's current state.

With [path], only records anchored to that file or directory (or a parent
or child of it) are shown. Without it, every record is listed. Newest first.

Records are written by the 'narrate' skill when a commit carries a signal:
an 'Incident:' or 'Decision:' trailer, a revert, or a change to an ADR.
They are deliberately tiny — a title, the paths, one paragraph, and a
pointer to the external ticket if there is one — never a copy of it.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) == 1 {
				query = args[0]
			}
			return runNexusHistory(cmd, query, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output structured JSON instead of plain text")
	return cmd
}

func runNexusHistory(cmd *cobra.Command, query string, asJSON bool) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		cmd.SilenceUsage = true
		fmt.Fprintln(cmd.ErrOrStderr(), "Not a git repository. Run 'nexus history' from inside a git repository.")
		return NewSilentError(fmt.Errorf("resolve worktree root: %w", err))
	}

	result, err := computeNexusHistory(ctx, repoRoot, query)
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

	if result.Error != "" {
		fmt.Fprintln(out, result.Error)
		return nil
	}
	if result.Count == 0 {
		if query == "" {
			fmt.Fprintf(out, "No history records in %q yet.\n", result.ExplainerBranch)
		} else {
			fmt.Fprintf(out, "No history records for %s.\n", query)
		}
		return nil
	}
	for i, e := range result.Entries {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s  %-8s  %s", e.Date, e.Kind, e.Title)
		if e.Ref != "" {
			fmt.Fprintf(out, "  [%s]", e.Ref)
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "    paths: %s\n", strings.Join(e.Paths, ", "))
		if e.Body != "" {
			fmt.Fprintf(out, "    %s\n", strings.ReplaceAll(e.Body, "\n", "\n    "))
		}
		if e.Link != "" {
			fmt.Fprintf(out, "    link: %s\n", e.Link)
		}
	}
	return nil
}

// computeNexusHistory resolves the history records relevant to query.
// Shared by 'nexus history' and the nexus_history MCP tool, same
// drift-proofing reason as computeNexusShow. Every "normal" outcome —
// Nexus not set up, branch missing, no records — is reported via the
// result, never a Go error.
func computeNexusHistory(ctx context.Context, repoRoot, query string) (nexusHistoryResult, error) {
	if _, statErr := os.Stat(filepath.Join(repoRoot, nexusSettingsFile)); statErr != nil {
		return nexusHistoryResult{Path: query, Error: "Nexus isn't set up in this repo. Run 'nexus init' first."}, nil
	}

	repo, err := gitrepo.OpenCurrent(ctx)
	if err != nil {
		return nexusHistoryResult{}, fmt.Errorf("open git repository: %w", err)
	}
	defer repo.Close()

	tree, explainerBranch, err := resolveExplainerTree(repo, repoRoot)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nexusHistoryResult{
				Path:            query,
				ExplainerBranch: explainerBranch,
				Error:           fmt.Sprintf("Branch %q not found. Run 'nexus init' first.", explainerBranch),
			}, nil
		}
		return nexusHistoryResult{}, err
	}

	entries, err := collectNexusHistory(tree, query)
	if err != nil {
		return nexusHistoryResult{}, fmt.Errorf("scan %s for history records: %w", explainerBranch, err)
	}
	if entries == nil {
		entries = []nexusHistoryEntry{}
	}
	return nexusHistoryResult{
		Path:            query,
		ExplainerBranch: explainerBranch,
		Count:           len(entries),
		Entries:         entries,
	}, nil
}
