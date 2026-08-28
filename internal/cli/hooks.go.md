---
path: "internal/cli/hooks.go"
summary: "Installs or extends the repo's post-commit git hook so every commit gets queued for narration."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/hooks.go

## What this does
`ensureNexusPostCommitHook` makes sure `.git/hooks/post-commit` will invoke
`nexus __post-commit-hook` after every commit, so each one gets queued for
narration in `.nexus/pending.json`.

## How it works
It reads whatever hook already exists (if any) and checks for a fixed
marker comment to stay idempotent — re-running `nexus init` won't add the
line twice. A missing hook gets a fresh one written with a `#!/bin/sh`
shebang. An existing hook only gets the line *appended*, and only if its
shebang is a POSIX shell it recognizes (`sh`/`bash`, plain or via
`env`) — appending shell syntax to a Python or Node hook would corrupt it,
so in that case it returns a warning telling the user to add the line by
hand instead. It never backs up or replaces an existing hook: post-commit
can't block a commit either way (the commit object already exists by the
time it runs), so appending is always safe, and a repo may already have
other tooling using the same hook.

## Recent changes
- Initial port from entireio/cli's nexus_hooks.go, updated for the standalone binary — the installed line now runs `nexus __post-commit-hook` instead of `entire nexus __post-commit-hook` (37415bf)
