---
path: "internal/jsonutil/write.go"
summary: "Writes a file atomically — via a temp file, fsync, and rename — so a crash mid-write never leaves a truncated config file behind."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/jsonutil/write.go

## What this does
`WriteFileAtomic` is how every config-ish file this codebase writes
(`.nexus/settings.json`, `.nexus/pending.json`, `.nexusignore`, and more)
actually gets written to disk, instead of a plain `os.WriteFile`.

## How it works
It writes to a temp file in the *same directory* as the target (so the
final rename stays on the same filesystem and is atomic), calls `Sync()`
on it before closing — without that, some filesystems can complete the
rename on disk while the file content itself is still sitting in a
write-back cache, so a crash right after could leave the rename "done" but
the file empty. It then `Chmod`s the temp file to the requested permission
before renaming into place (so the final file has the right permissions
regardless of what `CreateTemp` defaulted to), and does a best-effort
`fsync` on the *parent directory* afterward, since the rename's directory
entry needs its own fsync to be durable — without it, the file's bytes
could survive a crash while the directory still points at the old state.
The directory fsync is deliberately best-effort (and skipped-if-it-fails,
not skipped-if-unsupported): Windows doesn't support it at all, and by the
time it runs the rename has already succeeded, so failing the whole call
over it would be misleading.

## Recent changes
- Initial port from entireio/cli's jsonutil/write.go, unchanged apart from one `//nolint:errcheck` comment removed — it had become redundant once this repo's errcheck config was left at its default (which already doesn't flag a blank-assigned `_ = fn()` discard) (37415bf)
