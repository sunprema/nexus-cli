---
path: "internal/paths/worktree.go"
summary: "Resolves and caches the current git worktree root — the one place every command asks \"what repo am I in?\""
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/paths/worktree.go

## What this does
`WorktreeRoot` answers the question every `nexus` command starts with:
what repository (and specifically, which worktree of it) am I running in?
It's used to resolve every repo-relative path — `.nexus/settings.json`,
`.nexusignore`, and so on — regardless of which subdirectory the command
was actually invoked from.

## How it works
It shells out to `git rev-parse --show-toplevel`, which correctly returns
the *worktree's* root rather than the main repository's root when run
inside a linked worktree — a distinction that matters, since `.nexus/`
config lives per-worktree in spirit even though the underlying `.git` may
be shared. The result is cached, keyed by the current working directory,
so a command that calls `WorktreeRoot` more than once in a single run
doesn't re-shell-out each time. `ClearWorktreeRootCache` exists purely for
tests that change directories mid-run and need the cache invalidated.

## Recent changes
- Initial port: extracted from entireio/cli's larger paths.go, keeping only WorktreeRoot and its cache — the rest of that file (session-transcript path helpers, checkpoint metadata paths) is Entire-specific and has no equivalent here (37415bf)
