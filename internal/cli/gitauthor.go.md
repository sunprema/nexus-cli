---
path: "internal/cli/gitauthor.go"
summary: "Resolves the commit author identity Nexus uses for commits it makes itself, falling back to sensible defaults."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/gitauthor.go

## What this does
`gitAuthorFromRepo` figures out a name and email to sign a commit with when
Nexus creates one itself — right now, that's just the orphan commit that
initializes the `explainer` branch in `nexus init`.

## How it works
It first asks go-git for the merged local+global git config
(`repo.ConfigScoped`), which mirrors how `git` itself resolves identity
(local config wins over global). If either name or email is still missing,
it falls back to reading the global config directly. If both attempts come
up empty — no git identity configured anywhere — it defaults to
`"Unknown"` / `"unknown@local"` rather than failing the command outright;
an explainer-branch init commit isn't worth blocking on missing git config.

## Recent changes
- Initial port: inlined from entireio/cli's `checkpoint.GetGitAuthorFromRepo`, dropping the surrounding checkpoint/strategy packages that pulled in most of Entire's dependency graph (37415bf)
