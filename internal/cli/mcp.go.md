---
path: "internal/cli/mcp.go"
summary: "The `nexus mcp` command: a stdio Model Context Protocol server exposing explainer entries and guided tours as MCP tools for agents."
source_commit: bddecdf16a40ec36024e405738ac44087ef644a9
desynced: false
---

# internal/cli/mcp.go

## What this does
`nexus mcp` runs a read-only MCP (Model Context Protocol) server over
stdio, so an MCP-host agent can call `nexus_explainer`, `nexus_map`, and
`nexus_tour` as tools instead of shelling out to `nexus show`/`nexus
map`/`nexus tour` itself. All three tools are thin wrappers around the same
`compute*` functions those CLI commands already use (`computeNexusShow` in
`show.go`, `computeNexusMap` in `map.go`, `computeNexusTourShow` in
`tour.go`), so an MCP client and a shell script always see identical
path-mapping and branch resolution — neither surface can drift from the
other.

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

Each of the three tool handlers follows the same shape: resolve the repo
root, call the matching `compute*` function, and marshal the result as the
tool's text content. A tool that legitimately "found nothing" (no
explainer entry yet, no tour with that slug) is an ordinary successful
result, not an error — only an actual failure to open the repo or read a
blob is reported as a tool error (`isError: true` in the MCP response).

## Recent changes
- Initial port: adapted from entireio/cli's `entire mcp` command, keeping only the three nexus_* tools (nexus_explainer, nexus_map, nexus_tour) — dropped agent_help/entire_status, which have no equivalent in a standalone Nexus binary, and the strategy.WithFreshGitRemoteCache call, which was Entire-specific caching this repo doesn't need (bddecdf)
