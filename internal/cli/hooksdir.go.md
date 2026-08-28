---
path: "internal/cli/hooksdir.go"
summary: "Finds the current repository's git hooks directory by asking git directly."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/hooksdir.go

## What this does
`gitHooksDir` returns the path to `.git/hooks` (or wherever git actually
keeps hooks for this repo/worktree) so `nexus init` knows where to install
the post-commit hook.

## How it works
It shells out to `git rev-parse --git-path hooks` and trusts git's own
resolution rather than reimplementing worktree/`GIT_DIR` logic by hand.
Unlike Entire's equivalent (`strategy.GetHooksDir`), this doesn't cache the
result across calls — `nexus` is a one-shot CLI process that runs this at
most once or twice per invocation, so there's no repeated-call cost to
amortize a cache against.

## Recent changes
- Initial port: inlined and simplified from entireio/cli's `strategy.GetHooksDir`, dropping its per-process cache since it isn't needed here (37415bf)
