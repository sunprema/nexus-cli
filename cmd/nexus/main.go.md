---
path: "cmd/nexus/main.go"
summary: "The nexus binary's entrypoint: builds the root command, runs it, and prints an error only if it wasn't already shown to the user."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# cmd/nexus/main.go

## What this does
This is the whole `main()` for the `nexus` binary. It builds the Cobra root
command from `internal/cli` and executes it against a background context.
There's no other setup here — no flags parsed at this layer, no config
loaded — everything lives in the command tree itself.

## How it works
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
- Initial port from entireio/cli's `entire nexus` subcommand group into the standalone nexus-cli repo, as the new binary's entrypoint (37415bf)
