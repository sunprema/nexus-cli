---
path: "internal/cli/diff.go"
summary: "The `nexus diff <path>` command: a plain git diff between a file's last two narrated versions, no LLM involved."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/diff.go

## What this does
`nexus diff <path>` shows what the *last narration* actually changed for a
file, by finding the two most recent commits on the `explainer` branch
that touched `<path>.md` and printing a unified diff between them. This is
a "cheap tier" diff — pure git-object comparison, no model call — distinct
from asking an agent to re-explain anything.

## How it works
`lastTwoCommitsTouching` walks the explainer branch's commit log from its
tip with a path filter, equivalent to `git log -- <path>`, and stops as
soon as it has collected two commits (or reaches the end, meaning the file
has fewer than two narrated versions). It then diffs those two commits'
trees with go-git's own tree-diff machinery and prints the patch for just
the one file in question. Zero or one matching commits are reported as
plain status messages ("narrate this to create one" / "only narrated
once"), not errors.

## Recent changes
- Initial port from entireio/cli's nexus_diff.go, unchanged apart from import paths and the "nexus diff"/"nexus init" message wording (37415bf)
