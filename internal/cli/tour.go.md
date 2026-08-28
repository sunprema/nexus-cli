---
path: "internal/cli/tour.go"
summary: "Guided tours: cross-cutting, ordered walkthroughs of a codebase, stored as reserved files on the explainer branch."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/tour.go

## What this does
A "tour" is a curated, ordered list of files worth visiting to understand
some cross-cutting concern — not tied to any single code file the way a
per-file explainer entry is. Tours live under the reserved
`.nexus/tours/<slug>.md` prefix on the `explainer` branch. `nexus tour
<slug>` prints one tour's stops.

## How it works
Unlike a per-file explainer entry, a tour's frontmatter *is* its substance:
`title` and `stops` (each a path, optional line number, and a short note on
why it matters) — the Markdown body underneath is just optional framing
prose. `parseNexusTourFrontmatter` treats a tour with zero stops as not
being a tour at all (`ok = false`), so a malformed or empty tour file
doesn't show up in `nexus map` or resolve successfully here — there'd be
nothing useful to show either way. A stop deliberately doesn't duplicate
its target file's own explainer narrative; it just links to it by path, so
a tour stays cheap to keep in sync with the per-file entries it points at.

## Recent changes
- Initial port from entireio/cli's nexus_tour.go, unchanged apart from import paths and the "nexus tour"/"nexus init"/"nexus map" message wording (37415bf)
