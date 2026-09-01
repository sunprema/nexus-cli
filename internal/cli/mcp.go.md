---
path: "internal/cli/mcp.go"
summary: "The `nexus mcp` command: a stdio Model Context Protocol server exposing explainer entries, guided tours, and read-aloud (nexus_speak) as MCP tools for agents."
source_commit: fd3a7698657e8565a5fd672d9905e93f453c398b
desynced: false
---

# internal/cli/mcp.go

## What this does
`nexus mcp` runs an MCP (Model Context Protocol) server over stdio, so an
MCP-host agent can call `nexus_explainer`, `nexus_map`, `nexus_tour`, and
`nexus_speak` as tools instead of shelling out to `nexus show`/`nexus
map`/`nexus tour`/`nexus speak` itself. Every tool is a thin wrapper
around the same function its CLI command already uses (`computeNexusShow`
in `show.go`, `computeNexusMap` in `map.go`, `computeNexusTourShow` in
`tour.go`, `runNexusSpeak` in `speak.go`), so an MCP client and a shell
script always see identical behaviour — neither surface can drift from
the other.

The first three tools are read-only. `nexus_speak` is the one with a side
effect: it makes the person's computer read an explainer entry (or any
text the agent composes) aloud, for "read me the summary of auth.py".

## How it works
Transport is newline-delimited JSON-RPC 2.0, the MCP stdio framing:
`runMCPServer` scans stdin one line at a time (bounded to 1 MiB per line so
a malformed or abusive message can't exhaust memory), dispatches each
parsed request by method (`initialize`, `notifications/initialized`,
`ping`, `tools/list`, `tools/call`), and writes one JSON-RPC response per
line to stdout — except for a *notification* (a request with no `id`),
which per JSON-RPC gets no response at all. A request that's parseable
JSON but not a valid JSON-RPC 2.0 request (wrong `jsonrpc` version, no
`method`) is rejected with -32600 before dispatch, rather than falling
through to "method not found"; a request that isn't even parseable JSON —
including a batch array, which this server doesn't support — gets a -32700
parse error and the server recovers to read the next line rather than
terminating.

Each tool handler follows the same shape: resolve what it needs, call the
shared function, and marshal the result as the tool's text content. A
tool that legitimately "found nothing" (no explainer entry yet, no tour
with that slug, nothing to read aloud) is an ordinary successful result
carrying an explanation, not an error — only an actual failure (can't
open the repo, can't read a blob, no speech engine on this machine) is
reported as a tool error (`isError: true`). Missing required arguments
are tool errors too, not protocol errors, so the agent sees a readable
message instead of a JSON-RPC fault.

`nexus_speak` accepts `path` + `mode` (`summary` or `full`), or free
`text`, plus `voice`, `print` (return the prepared text instead of
speaking), and `stop` (interrupt whatever is playing). It always runs
detached: the tool returns as soon as audio starts, with an estimated
duration, because a full narrative can play for minutes and a blocked
tool call would stall the agent for all of it. The tool's description
tells the agent not to echo the spoken text back and not to queue more
until that time has passed — advice, since the server can't enforce it.

## Recent changes
- Added the `nexus_speak` tool — the first one with a side effect — wrapping `runNexusSpeak` in detached mode so an agent can read entries aloud without blocking; tool-call argument parsing grew `mode`, `text`, `voice`, `stop`, `print` (fd3a769)
- Initial port: adapted from entireio/cli's `entire mcp` command, keeping only the three nexus_* tools (nexus_explainer, nexus_map, nexus_tour) — dropped agent_help/entire_status, which have no equivalent in a standalone Nexus binary, and the strategy.WithFreshGitRemoteCache call, which was Entire-specific caching this repo doesn't need (bddecdf)
