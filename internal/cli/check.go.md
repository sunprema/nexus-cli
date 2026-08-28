---
path: "internal/cli/check.go"
summary: "The `nexus check` command, plus the shared explainer-branch/tree resolution logic every other read command builds on."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/check.go

## What this does
`nexus check` scans every Markdown file on the `explainer` branch for
unresolved desync markers — the callout the `narrate` skill writes when its
own verification step catches the narrative disagreeing with the code. It
never runs an LLM check itself and never clears markers; narrating the
affected file again is what resolves one.

This file also holds `resolveExplainerBranchRef`/`resolveExplainerTree`,
the one place that resolves the explainer branch's name (from
`.nexus/settings.json`, defaulting to `"explainer"`) and opens its tip
tree — shared by `check`, `show`, `diff`, `map`, and `tour` so none of them
can drift from each other about which branch or commit they're reading.

## How it works
For each Markdown file, `findDesyncedFiles` prefers a file's YAML
frontmatter `desynced` field when present (that's the
machine-authoritative signal — see `frontmatter.go`) and only falls back to
scanning for an inline `> **Nexus desync**` marker line for files narrated
before frontmatter existed. The marker match requires the *prefix* of a
trimmed line, not a substring anywhere in the file — this comment block
itself describes the marker text in prose, and a substring match would
flag this very file as desynced for merely mentioning the pattern.

## Recent changes
- Initial port from entireio/cli's nexus_check.go, unchanged apart from import paths, the "nexus check"/"nexus init" message wording, and doc-comment references from `nexus_frontmatter.go`/`nexus_show.go` to this repo's `frontmatter.go`/`show.go` (37415bf)
