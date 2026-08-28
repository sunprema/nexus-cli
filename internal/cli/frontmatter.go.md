---
path: "internal/cli/frontmatter.go"
summary: "Parses the YAML frontmatter every explainer file starts with, so a reader can get a file's gist without opening the full narrative."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/frontmatter.go

## What this does
Every explainer entry the `narrate` skill writes starts with a YAML
frontmatter block (`path`, `summary`, `source_commit`, `desynced`, and
optionally `tests`). This file is the one place that block gets parsed
back out, shared by `check`, `show`, and `map` so they all read the same
fields the same way.

## How it works
`splitNexusFrontmatter` matches a leading `---\n...\n---\n` block with a
non-greedy regex, so it stops at the *first* closing `---` rather than a
later one that might appear inside a Mermaid diagram or fenced code block
in the body. `parseNexusFrontmatter` then unmarshals that block into
`nexusExplainerFrontmatter`. If a file has no frontmatter block at all, or
the block fails to parse as YAML, this is treated as "no frontmatter" —
not an error — and the entire original content is returned as the body.
That's a deliberate degrade: it covers files narrated before this feature
existed, or ones hand-edited into a bad state, without crashing a caller
that just wants "the current status."

## Recent changes
- Initial port from entireio/cli's nexus_frontmatter.go, unchanged except a doc-comment reference to `nexus_tour.go` updated to this repo's `tour.go` (37415bf)
