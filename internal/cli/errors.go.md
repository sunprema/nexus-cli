---
path: "internal/cli/errors.go"
summary: "A wrapper error type that tells main.go \"I already printed this, don't print it again.\""
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/errors.go

## What this does
Defines `SilentError`, a small `error` wrapper with an `AlreadyPrinted()`
marker method, and `NewSilentError` to construct one. Commands use this
when they've already written a user-facing message (like "Not a git
repository...") and don't want the generic error text repeated underneath
it.

## How it works
`SilentError` implements the standard `error` interface (`Error()`) plus
`Unwrap()` so `errors.As`/`errors.Is` still see through it, and
`AlreadyPrinted() bool` which always returns `true` — it exists purely as a
type marker `main.go` can detect. A command that hits a known,
already-explained failure mode returns `NewSilentError(err)` instead of the
raw error; `main.go` checks for that shape before deciding whether to print
anything.

## Recent changes
- Initial port from entireio/cli's errors.go, trimmed to just SilentError — the original file also carried RenderUserFacingError for the control-plane client's error wrapping, which has no equivalent here (37415bf)
