---
path: "internal/gitrepo/alternates_rewrite.go"
summary: "Intercepts reads of objects/info/alternates and rewrites any relative entries to absolute paths, since go-git can't follow relative alternates itself."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/gitrepo/alternates_rewrite.go

## What this does
`alternatesRewriteFS` wraps the git-dir filesystem go-git reads config and
metadata through. It's transparent for almost every file, except one: when
something tries to open `objects/info/alternates` itself, it serves a
rewritten copy with every relative entry resolved to an absolute path
against `<repo>/objects`, instead of the raw file on disk.

## How it works
`absolutizedAlternates` reads the wrapped filesystem's real alternates
file, capped at `maxAlternatesReadBytes` (so a pathological alternates
file can't be read unbounded into memory), and hands it to
`rewriteRelativeAlternates`, which walks each line: blank lines, comments
(`#`-prefixed), and already-absolute entries are left untouched, while a
relative one is rewritten in place. If nothing needed rewriting *and* the
file fit within the cap, the original is served unchanged — but if the
file was truncated by the cap, the rewritten copy is always served, even
if no visible entry needed a rewrite, because a relative entry could exist
past the cutoff that go-git would otherwise mangle by reading the raw,
untruncated file some other way. `inMemoryFile` (at the bottom of this
file) hands back the rewritten content as an in-memory `billy.File` via
`go-git-billy`'s `memfs`, standing in for the real file transparently to
whatever opened it.

## Recent changes
- Initial port from entireio/cli's gitrepo/alternates_rewrite.go, unchanged apart from dropping the `//nolint:wrapcheck` comments now that this repo's lint config doesn't enable wrapcheck — replaced with plain comments preserving the same rationale (37415bf)
