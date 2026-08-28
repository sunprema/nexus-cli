---
path: "internal/cli/ignore.go"
summary: "Writes the default .nexusignore file, pre-populated with lockfiles and common test-file patterns."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/ignore.go

## What this does
`ensureNexusIgnore` creates `.nexusignore` at the repo root (only if it
doesn't already exist) with a starter set of gitignore-syntax patterns for
paths the `narrate` skill should skip.

## How it works
The default content excludes common lockfiles (package-lock.json, go.sum,
Cargo.lock, and similar) since they're machine-generated with nothing
authorial to narrate, plus common test-file naming conventions across
several languages. Test exclusions are opinionated but deliberate: a test's
own name and assertions usually already say what it checks, so narrating
it duplicates that for little reviewer value — teams that disagree can just
delete those lines, since the file is meant to be team-edited afterward.
Structural exclusions (`.nexus/`, `.git*`) are intentionally *not* listed
here — those are hardcoded in the `narrate` skill itself and can't be
overridden, since Nexus narrating its own config would be nonsensical.

## Recent changes
- Initial port from entireio/cli's nexus_ignore.go, unchanged apart from the import path for jsonutil (37415bf)
