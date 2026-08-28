---
path: "internal/jsonutil/json.go"
summary: "Two JSON marshal helpers that keep formatting consistent across the codebase: no HTML escaping, and a trailing newline where one is wanted."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/jsonutil/json.go

## What this does
`MarshalIndentWithNewline` and `MarshalWithNoHTMLEscape` are small
wrappers around the standard `encoding/json` encoder, used everywhere this
codebase writes JSON so formatting stays consistent without every call
site repeating the same setup.

## How it works
Both disable HTML escaping (`SetEscapeHTML(false)`) — plain Go `json.Marshal`
escapes `<`, `>`, and `&` by default, which is meant for embedding JSON in
HTML but is just visual noise (and occasional confusion) in a config file
or CLI output nobody's putting in a browser. `MarshalIndentWithNewline`
additionally pretty-prints with the given prefix/indent and keeps the
encoder's trailing newline, which config files like `.nexus/settings.json`
want for a clean POSIX line ending. `MarshalWithNoHTMLEscape` does the
opposite for the newline — it strips the one the encoder adds, matching
plain `json.Marshal`'s behavior for callers that don't want it appended.

## Recent changes
- Initial port from entireio/cli's jsonutil/json.go, unchanged (37415bf)
