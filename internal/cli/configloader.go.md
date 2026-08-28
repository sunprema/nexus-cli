---
path: "internal/cli/configloader.go"
summary: "Overrides go-git's config loader so global git config behind a symlinked directory (chezmoi, Stow, yadm) still gets read."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/configloader.go

## What this does
Registers a replacement for go-git's default global-config loader at
program startup, so `gitauthor.go`'s config lookup doesn't silently drop a
user's identity when their `~/.config` is a symlink managed by a dotfile
tool.

## How it works
go-git's built-in loader reads config files through Go's `os.Root`, which
refuses to follow an absolute symlink anywhere in the path — even one that
resolves back inside the intended root — and fails with "path escapes from
parent". `osSymlinkFS` is a minimal filesystem shim backed directly by the
plain `os` package instead, which follows symlinks the same way `git`
itself does. An `init()` function registers it as the active `ConfigLoader`
plugin before any command runs. Without this, a user whose `~/.config` is a
symlink would have every commit Nexus makes attributed to "Unknown" instead
of their real git identity.

## Recent changes
- Initial port from entireio/cli's checkpoint/configloader.go verbatim — it has no dependency on anything else in that package, so it ported unchanged (37415bf)
