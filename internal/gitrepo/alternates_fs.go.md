---
path: "internal/gitrepo/alternates_fs.go"
summary: "An OS-rooted billy.Filesystem that lets go-git follow an object-alternates path outside the repo's own .git directory."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/gitrepo/alternates_fs.go

## What this does
`alternatesFilesystem` is the filesystem go-git's object storage uses to
actually *read* objects once `objects/info/alternates` has pointed it at
another repository's object store — which is very likely to be an absolute
path outside the current repo's `.git` entirely.

## How it works
It's a `billy.Filesystem` implementation rooted at the OS's real
filesystem root (`os.PathSeparator`, via `osfs`), so every path it resolves
is treated as absolute rather than relative to some inner root the way a
normal `.git`-scoped filesystem would. Every method is a thin passthrough
to an underlying OS filesystem after `resolve()` turns the requested name
into an absolute, cleaned path — deliberately preserving whatever error
the OS filesystem returns rather than wrapping it, since go-git relies on
recognizing `os.IsNotExist` on that exact error. The one non-trivial piece
is `Open`: if the path being opened is itself a *nested* alternates file
(an alternate repository that has its own alternates), it reads and
rewrites that file's relative entries the same way the top-level one is
rewritten, so chained alternates resolve correctly too.

## Recent changes
- Initial port from entireio/cli's gitrepo/alternates_fs.go verbatim, along with the rest of the gitrepo package (37415bf)
