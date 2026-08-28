---
path: "internal/cli/post_commit_hook.go"
summary: "The hidden command the installed git hook actually calls after every commit, to queue it for narration."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/post_commit_hook.go

## What this does
Implements `nexus __post-commit-hook`, a hidden command not meant to be run
by hand — it's what the post-commit hook `nexus init` installs actually
invokes. It appends the current commit to `.nexus/pending.json`, and
that's all.

## How it works
Every exit path here is a silent no-op, never a returned error: a
post-commit hook can't abort a commit (the commit object already exists by
the time it runs), so failing loudly on every commit would just be noise.
It bails out quietly if Nexus isn't set up in this repo, and also — this is
the one subtle case — if `HEAD` is currently on the `explainer` branch
itself. Git hooks are shared across all worktrees of a repo, so this same
hook fires when the `narrate` skill commits its own narration in the
explainer worktree; without this check, Nexus would end up queuing its own
narration commits for narration, forever.

## Recent changes
- Initial port from entireio/cli's nexus_post_commit_hook.go, unchanged apart from import paths (37415bf)
