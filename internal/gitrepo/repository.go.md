---
path: "internal/gitrepo/repository.go"
summary: "The single place that opens a git repository, with support for shared-clone object alternates and the reftable ref backend that a plain go-git open can't handle."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/gitrepo/repository.go

## What this does
`OpenCurrent` and `OpenPath` are the only entry points anything in this
repo uses to open a git repository — a rule enforced by convention, not the
compiler, but important: opening one with go-git's plain `git.PlainOpen`
instead silently breaks on two kinds of repositories that are otherwise
completely normal.

## How it works
The first problem is object alternates. A repo cloned with `--shared` (or
similar) doesn't store its own objects — it points at another repo's
object store via `objects/info/alternates`, sometimes with a *relative*
path. go-git's plain open can't follow a relative alternate at all, so
`openPathWithAlternates` wraps the git-dir filesystem in a layer
(`alternates_rewrite.go`) that rewrites relative entries to absolute paths
on read, and swaps in an OS-rooted filesystem (`alternates_fs.go`) so
go-git can actually reach objects living outside `.git`.

The second is the reftable ref backend (git 2.45+, opt-in). go-git's
filesystem storer reads refs from `.git/refs`, `packed-refs`, and `HEAD` —
none of which are authoritative once a repo has migrated to reftable, and
opening one directly fails outright with "unknown extension: refstorage".
`repoUsesReftable` detects this by checking for a `reftable/` directory,
and if found, ref operations are routed through `reftableStorer`
(`reftable.go`), which shells out to the git CLI for every ref read/write
while object storage stays on go-git as normal.

`OpenPath` falls back to plain `git.PlainOpen` only when neither of the
above applies — a repo that genuinely has no alternates is allowed to
silently degrade to the simpler path, but one that does is not: a wrapped
open failing on an alternates-carrying repo returns an error rather than
quietly opening in a broken state.

## Recent changes
- Initial port from entireio/cli's gitrepo/repository.go, unchanged except the `paths` import path — carried over in full (not simplified to just OpenCurrent) once it became clear this file's ~700-line support cast (alternates + reftable) can't be dropped without losing real correctness for shared clones and reftable repos (37415bf)
