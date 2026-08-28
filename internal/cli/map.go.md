---
path: "internal/cli/map.go"
summary: "The `nexus map` command: a one-line index of every narrated file and guided tour, built entirely from frontmatter."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/map.go

## What this does
`nexus map` walks the whole `explainer` branch and prints one line per
entry: a code file's path and summary (flagged if desynced), or a guided
tour's slug, title, and stop count. It's meant for getting the gist of an
unfamiliar codebase — or picking which file or tour is worth reading in
full — the same way scanning SKILL.md descriptions beats opening every
skill.

## How it works
It walks every `.md` file on the branch's tree. A path under the reserved
`.nexus/tours/` prefix is treated as a guided tour and parsed with
`parseNexusTourFrontmatter` (see `tour.go`); anything else is treated as a
per-file explainer entry and parsed with `parseNexusFrontmatter`. A file's
*path* always comes from its actual position in the git tree, not from
whatever `path:` its frontmatter happens to claim — the tree is ground
truth for what a file mirrors, while frontmatter is descriptive metadata
that could in principle drift. Files narrated before frontmatter existed
still get listed, just without a summary.

## Recent changes
- Initial port from entireio/cli's nexus_map.go, unchanged apart from import paths and the "nexus map"/"nexus init"/"nexus tour" message wording (37415bf)
