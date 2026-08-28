---
path: "internal/cli/pending.go"
summary: "Reads and writes .nexus/pending.json, the queue of commits still waiting to be narrated."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/pending.go

## What this does
Owns `.nexus/pending.json`: the list of commit SHAs the post-commit hook
has queued for narration but the `narrate` skill hasn't drained yet. Four
operations: load, save, append (idempotent — won't double-queue a commit),
and remove (used once a commit has actually been narrated).

## How it works
It's plain JSON I/O with no cleverness: `loadNexusPending` tolerates a
missing file (returns an empty queue rather than an error, since a fresh
repo won't have one yet), and `saveNexusPending` writes atomically via
`jsonutil.WriteFileAtomic` so a crash mid-write can't leave a corrupt
queue. This file is local, transient state — `.nexus/.gitignore` excludes
it from commits, unlike `settings.json` and the narrator prompt, which are
team-shared config.

## Recent changes
- Initial port from entireio/cli's nexus_pending.go, unchanged apart from the jsonutil import path (37415bf)
