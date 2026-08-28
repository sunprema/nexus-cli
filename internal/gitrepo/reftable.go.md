---
path: "internal/gitrepo/reftable.go"
summary: "Routes reference reads/writes through the git CLI for a reftable-backed repository, since go-git's own storer can't read that format."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/gitrepo/reftable.go

## What this does
`reftableStorer` wraps go-git's normal filesystem storage and overrides
just the reference-related methods (`Reference`, `SetReference`,
`CheckAndSetReference`, `IterReferences`, `RemoveReference`, and the
loose-ref bookkeeping methods) to shell out to the `git` CLI instead —
object, config, index, shallow, and module storage are untouched and keep
flowing through the embedded filesystem storage as normal. It also
overrides `SupportsExtension`, shadowing the embedded storage's method so
go-git's extension check accepts `extensions.refstorage=reftable` instead
of rejecting the repository outright, while still delegating to the
embedded implementation for every other extension it recognizes (object
format, worktree config). `repoUsesReftable` (also in this file) detects
whether a repository needs this at all, by checking for a `reftable/`
directory under the git dir or shared common dir.

## How it works
Every ref operation becomes a `git` subprocess call (`symbolic-ref`,
`rev-parse --verify`, `update-ref`, `for-each-ref`) run with a 30-second
timeout and a forced `LC_ALL=C`/`LANG=C` environment so git's diagnostic
text stays untranslated — several classification helpers
(`refLookupAbsent`, `isRefCASConflict`) parse that text to distinguish
"ref genuinely doesn't exist" from "git failed to run at all," and a
localized machine would silently break that distinction. `Reference` and
`IterReferences` both have to probe for a *symbolic* ref (like `HEAD` on a
branch) before falling back to a hash lookup, since `git` reports "not a
symbolic ref" the same way it reports "ref not found" — only a genuinely
empty-stderr, non-zero exit counts as "not symbolic," anything else (a
timeout, a spawn failure) is surfaced as a real error rather than silently
misreading a symbolic `HEAD` as detached. `CheckAndSetReference` implements
go-git's atomic compare-and-swap semantics by passing the old value to
`update-ref`, and only maps `git`'s specific "but expected"/"already
exists" stderr text to the CAS-conflict sentinel — anything else (a bad
object, a lock, a genuine failure) is surfaced as itself rather than
misreported as a benign race.

## Recent changes
- Initial port from entireio/cli's gitrepo/reftable.go verbatim, with one addition: a `//nolint:gosec` comment on the `exec.CommandContext` call, needed because this repo's lint config enables gosec's G204 check on a fixed "git" binary invocation (37415bf)
