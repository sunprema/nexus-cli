package cli

import (
	"regexp"

	"gopkg.in/yaml.v3"
)

// nexusFrontmatterPattern matches a leading YAML frontmatter block: an
// opening "---" line, the block (non-greedy, so it stops at the FIRST
// closing delimiter rather than a later "---" that might appear inside a
// mermaid diagram or code fence in the body), a closing "---" line, and
// everything after as the body. (?s) makes "." match newlines so the
// frontmatter block itself can span multiple lines.
var nexusFrontmatterPattern = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?(.*)$`)

// nexusExplainerFrontmatter is the YAML frontmatter the 'narrate' skill
// writes at the top of every explainer file — see
// docs/adr/0004-explainer-frontmatter.md. Lets a reader (human or agent)
// get a file's gist, source commit, and desync status without opening the
// full narrative, the same way a SKILL.md's frontmatter lets an agent
// decide relevance without reading the whole skill body.
type nexusExplainerFrontmatter struct {
	Path         string `yaml:"path"`
	Summary      string `yaml:"summary"`
	SourceCommit string `yaml:"source_commit,omitempty"`
	Desynced     bool   `yaml:"desynced"`
	// Tests is set only when narrate recognizes this file as a test file:
	// one entry per test function, naming what it actually verifies rather
	// than restating its assertions. Deliberately lives on the same
	// per-file entry as everything else here, not a separate artifact —
	// a test file's tests are inherently about that one file, unlike a
	// tour (tour.go), which is cross-cutting by design.
	Tests []nexusTestIntent `yaml:"tests,omitempty"`
}

// nexusTestIntent is one test function's entry in a test file's Tests list.
// Name is the test function's own identifier — Go's "TestRetry_Transient",
// pytest's "test_retry_transient", a Jest "it(...)" string — whatever the
// language's natural unique name is, since that's what a reader (or a
// future CodeLens matching stops to source lines) looks up by.
type nexusTestIntent struct {
	Name   string `yaml:"name" json:"name"`
	Intent string `yaml:"intent" json:"intent"`
}

// splitNexusFrontmatter extracts the raw (unparsed) YAML block and body from
// content. ok is false when content doesn't start with a "---" block at
// all — the only case both frontmatter parsers below need to distinguish
// before attempting their own typed yaml.Unmarshal.
func splitNexusFrontmatter(content string) (raw, body string, ok bool) {
	m := nexusFrontmatterPattern.FindStringSubmatch(content)
	if m == nil {
		return "", content, false
	}
	return m[1], m[2], true
}

// parseNexusFrontmatter splits an explainer file's content into its parsed
// frontmatter and body. hasFrontmatter is false when content doesn't start
// with a "---" block at all, or when that block fails to parse as YAML —
// both cases most commonly mean the file was narrated before this feature
// existed, or was hand-edited into a bad state. Either way this returns the
// zero frontmatter value and the ENTIRE original content as body, so a
// caller that only wants "the current status" degrades to "unknown,
// nothing to report" rather than erroring, and a caller displaying content
// still shows something sensible.
func parseNexusFrontmatter(content string) (fm nexusExplainerFrontmatter, body string, hasFrontmatter bool) {
	raw, splitBody, split := splitNexusFrontmatter(content)
	if !split {
		return nexusExplainerFrontmatter{}, content, false
	}
	if err := yaml.Unmarshal([]byte(raw), &fm); err != nil {
		return nexusExplainerFrontmatter{}, content, false
	}
	return fm, splitBody, true
}
