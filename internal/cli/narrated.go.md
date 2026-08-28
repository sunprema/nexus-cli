---
path: "internal/cli/narrated.go"
summary: "The `nexus narrated <commit>` command: removes a commit from the pending queue once it's been narrated."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/narrated.go

## What this does
`nexus narrated <commit>` is what the `narrate` skill calls right after it
commits an explainer entry for `<commit>`, to dequeue it from
`.nexus/pending.json`. It only edits the pending queue — it never touches
git history.

## How it works
The interesting part is `resolveCommitSHA`: the pending queue always keys
entries on the exact string `repo.Head().Hash().String()` produced when the
commit was queued, so a lookup by any other form (a short SHA, a branch
name, `HEAD`) has to be normalized to match. It does that by shelling out
to `git rev-parse --verify <commitish>^{commit}`, which both validates the
ref exists and expands it to the full SHA. If the commit was already
removed from the queue (or never queued — e.g. narrated proactively before
the hook ran), the command reports that plainly rather than treating it as
an error.

## Recent changes
- Initial port from entireio/cli's nexus_narrated.go, unchanged apart from import paths (37415bf)
