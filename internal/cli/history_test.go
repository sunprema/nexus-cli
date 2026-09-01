package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

const validHistoryRecord = `---
kind: incident
title: "Desync detector flagged its own explainer entry"
date: 2026-08-26
source_commit: 0e791d5000000000000000000000000000000000
paths:
  - internal/cli/check.go
ref: "ADR-0004"
link: "https://example.invalid/adr/0004"
---
The marker scan matched prose that merely described the marker. Match only
lines that start with it; don't loosen that.
`

func TestParseNexusHistoryFrontmatter_Valid(t *testing.T) {
	t.Parallel()
	fm, body, ok := parseNexusHistoryFrontmatter(validHistoryRecord)
	if !ok {
		t.Fatal("expected a valid record")
	}
	if fm.Kind != "incident" || fm.Title == "" || fm.Ref != "ADR-0004" || fm.Link == "" {
		t.Errorf("frontmatter not parsed as expected: %+v", fm)
	}
	// An unquoted YYYY-MM-DD must survive as a string, not be coerced into
	// a timestamp the narrate skill never wrote.
	if fm.Date != "2026-08-26" {
		t.Errorf("date = %q, want 2026-08-26", fm.Date)
	}
	if len(fm.Paths) != 1 || fm.Paths[0] != "internal/cli/check.go" {
		t.Errorf("paths = %v", fm.Paths)
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "The marker scan") {
		t.Errorf("body not split correctly: %q", body)
	}
}

// The scope fence: a record with no anchoring path, or no title, is not a
// record at all. An empty kind is tolerated and defaults to "note".
func TestParseNexusHistoryFrontmatter_Rejections(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"no frontmatter": "just prose\n",
		"bad yaml":       "---\ntitle: [unclosed\npaths: x\n---\nbody\n",
		"no paths":       "---\ntitle: \"t\"\nkind: decision\n---\nbody\n",
		"empty paths":    "---\ntitle: \"t\"\npaths: []\n---\nbody\n",
		"no title":       "---\nkind: decision\npaths:\n  - a.go\n---\nbody\n",
	}
	for name, content := range cases {
		if _, _, ok := parseNexusHistoryFrontmatter(content); ok {
			t.Errorf("%s: expected ok=false", name)
		}
	}

	fm, _, ok := parseNexusHistoryFrontmatter("---\ntitle: \"t\"\npaths:\n  - a.go\n---\nbody\n")
	if !ok || fm.Kind != "note" {
		t.Errorf("empty kind should default to note, got ok=%v kind=%q", ok, fm.Kind)
	}
}

func TestNexusHistoryPathMatches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		query, anchor string
		want          bool
	}{
		{"src/auth.py", "src/auth.py", true},
		{"src/auth.py", "src", true},           // anchored to a parent dir
		{"src/auth.py", "src/", true},          // trailing slash tolerated
		{"src", "src/auth.py", true},           // asking about a dir finds its files
		{"src/auth.py", ".", true},             // repo-wide record
		{"src/auth.py", "src/authz.py", false}, // sibling, not a prefix match
		{"src/auth.py", "src/auth", false},     // "src/auth" is not a parent of "src/auth.py"
		{"lib/auth.py", "src", false},          // different tree
		{"./src/auth.py", "src/auth.py", true}, // cleaned
		{"src/auth.py", "src/../src/auth.py", true},
	}
	for _, c := range cases {
		if got := nexusHistoryPathMatches(c.query, c.anchor); got != c.want {
			t.Errorf("matches(%q, %q) = %v, want %v", c.query, c.anchor, got, c.want)
		}
	}
}

func TestSortNexusHistory_NewestFirst(t *testing.T) {
	t.Parallel()
	entries := []nexusHistoryEntry{
		{ID: "2026-01-01-a", Date: "2026-01-01"},
		{ID: "2026-03-01-b", Date: "2026-03-01"},
		{ID: "2026-03-01-c", Date: "2026-03-01"},
		{ID: "2025-12-01-d", Date: "2025-12-01"},
	}
	sortNexusHistory(entries)
	got := []string{entries[0].ID, entries[1].ID, entries[2].ID, entries[3].ID}
	want := []string{"2026-03-01-c", "2026-03-01-b", "2026-01-01-a", "2025-12-01-d"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// nexus_history over MCP returns nexusHistoryResult JSON; no path means
// "every record" and is not an error.
func TestMCPServer_HistoryTool(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":30,"method":"tools/call","params":{"name":"nexus_history","arguments":{}}}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	if resps[0].Error != nil {
		t.Fatalf("unexpected rpc error: %+v", resps[0].Error)
	}
	text := mcpResultText(t, resps[0].Result)
	var res nexusHistoryResult
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatalf("nexus_history tool should return nexusHistoryResult JSON, got %q (err %v)", text, err)
	}
	if res.Error == "" && res.Entries == nil {
		t.Errorf("entries should be an empty list, not null, when nothing is recorded: %s", text)
	}
}

// nexus_explainer embeds the same per-path records, so an agent reading a
// file's explainer sees its history without a second call.
func TestMCPServer_ExplainerToolCarriesHistoryField(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":31,"method":"tools/call","params":{"name":"nexus_explainer","arguments":{"path":"internal/cli/check.go"}}}`)
	if len(resps) != 1 || resps[0].Error != nil {
		t.Fatalf("unexpected response: %+v", resps)
	}
	text := mcpResultText(t, resps[0].Result)
	var res map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Only present when there are records (omitempty) — either shape is
	// fine, but if present it must decode as a list of entries.
	if raw, ok := res["history"]; ok {
		var entries []nexusHistoryEntry
		if err := json.Unmarshal(raw, &entries); err != nil {
			t.Errorf("history field should be a list of entries: %v", err)
		}
	}
}
