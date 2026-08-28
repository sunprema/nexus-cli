---
path: "internal/versioninfo/versioninfo.go"
summary: "Resolves the running nexus binary's version and commit, from goreleaser ldflags on a release build or Go's own build info otherwise."
source_commit: bddecdf16a40ec36024e405738ac44087ef644a9
desynced: false
---

# internal/versioninfo/versioninfo.go

## What this does
Exposes `Version` and `Commit`, used by `nexus --version` and the MCP
server's `serverInfo`. A release binary (built by goreleaser) has these
stamped in at link time via `-X`; every other way of getting a `nexus`
binary — `go build`, `go install .../nexus@<version>`, a plain local build
— carries no such stamp, so `Load()` recovers real values from Go's own
embedded build info instead. Either way, the binary reports something
meaningful rather than a bare "dev".

## How it works
`Version`/`Commit` default to `"dev"`/`"unknown"`. `Load()` (called once,
early in `main()`) only *fills in* those defaults from
`runtime/debug.ReadBuildInfo()` — it never overwrites a value ldflags
already stamped, so a release build's explicit version always wins. When
falling back to build info: a binary installed via `go install
...@<version>` carries the version as `info.Main.Version` (the `v` prefix
is trimmed to match goreleaser's own `{{.Version}}` format); a plain local
build instead reports `"(devel)"` there, so the commit is recovered from
the `vcs.revision` build setting instead — Go already appends `+dirty` to
that when the tree has uncommitted changes, so there's no separate
"modified" flag to track here.

## Recent changes
- Initial port from entireio/cli's versioninfo package, keeping only Version/Commit/Load — dropped UserAgent() and the HTTP transport-wrapping helpers, since this repo makes no outbound HTTP calls (bddecdf)
