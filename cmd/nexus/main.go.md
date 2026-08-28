---
path: "cmd/nexus/main.go"
summary: "The nexus binary's entrypoint: resolves the build version, builds the root command, runs it, and prints an error only if it wasn't already shown to the user."
source_commit: bddecdf16a40ec36024e405738ac44087ef644a9
desynced: false
---

# cmd/nexus/main.go

## What this does
This is the whole `main()` for the `nexus` binary. It resolves the running
build's version via `versioninfo.Load()`, builds the Cobra root command from
`internal/cli`, and executes it against a background context. There's no
other setup here — no flags parsed at this layer, no config loaded —
everything lives in the command tree itself.

## How it works
`versioninfo.Load()` runs first, before the root command is even
constructed, because `NewRootCmd` reads `versioninfo.Version` to populate
Cobra's `Version` field (backing `nexus --version`) — if `Load()` ran later,
the root command would already have captured whatever default was in place
at construction time.

Cobra commands already print their own errors by default, but `nexus`'s
commands sometimes print a friendlier, custom message themselves (e.g. "Not
a git repository...") and don't want Cobra's generic error text duplicated
underneath it. To avoid that, a command that has already printed its own
message returns a `*cli.SilentError`. `main` checks for that via a small
local `silentError` interface (anything with `Error()` and
`AlreadyPrinted() bool`) and `errors.As` — if the returned error satisfies
it, `main` stays quiet; otherwise it prints the error to stderr itself.
Either way, a non-nil error exits with status 1.

## Recent changes
- Added the `versioninfo.Load()` call so the build's version/commit are resolved before the root command reads them (bddecdf)
- Initial port from entireio/cli's `entire nexus` subcommand group into the standalone nexus-cli repo, as the new binary's entrypoint (37415bf)
