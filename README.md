# Nexus

Nexus decouples the **Machine Implementation** (code) from the **Human
Intent** (narrative) by maintaining a synchronized, human-readable shadow
branch alongside your code. Instead of reviewing a code diff, a reviewer
reads a short, jargon-free explanation of what changed and why.

- **`main`** — your code, unchanged. Always the source of truth.
- **`explainer`** — an orphan branch mirroring `main`'s file structure, one
  Markdown file per code file, narrating intent instead of syntax.

This repo is the standalone `nexus` CLI: it manages the `explainer` branch
(`nexus init`, `nexus show`, `nexus map`, ...) and ships the `narrate`
skill that a coding agent uses to actually write the narrative. It does not
depend on any other project — a plain `git` repository is all it needs.

## Install

```bash
go install github.com/sunprema/nexus-cli/cmd/nexus@latest
```

Or build from source:

```bash
git clone https://github.com/sunprema/nexus-cli.git
cd nexus-cli
go build -o nexus ./cmd/nexus
```

Prebuilt binaries for macOS/Linux/Windows are attached to each
[GitHub release](https://github.com/sunprema/nexus-cli/releases).

## Usage

```bash
nexus init      # create the 'explainer' branch, .nexus/ config, and the post-commit hook
nexus sync       # list commits queued for narration
nexus show <path>   # print a file's current explainer entry
nexus map        # index every narrated file and guided tour
nexus diff <path>   # diff a file's last two narrated versions
nexus check       # report files still flagged with a desync marker
nexus tour <slug>   # print a guided tour's stops
```

`nexus init` installs a post-commit git hook that queues every commit for
narration in `.nexus/pending.json` — cheaply, with no LLM call. Narration
itself happens later, via the `narrate` skill running inside your coding
agent (see below): it drains that queue, writes the explainer entries, and
verifies each one against the code with an independent pass before
committing.

Run `nexus <command> --help` for full flag and argument details.

## MCP server

`nexus mcp` runs a read-only [Model Context Protocol](https://modelcontextprotocol.io)
server over stdio, exposing the explainer branch as three tools instead of
requiring an agent to shell out to `nexus` and parse its output:

- **`nexus_explainer`** — a code file's current explainer entry (same data
  as `nexus show <path> --json`). An agent should call this *before*
  reading or editing a file: the explainer often already captures intent
  and edge cases that would otherwise have to be re-derived from the code.
- **`nexus_map`** — the whole-branch index of every narrated file and
  guided tour (same data as `nexus map --json`). Meant to be called first
  when orienting in an unfamiliar repo — the cheapest way to learn what's
  there before reading any code.
- **`nexus_tour`** — one guided tour's ordered stops (same data as
  `nexus tour <slug> --json`).

All three share the exact same lookup code the CLI commands use, so an MCP
client and a shell script can never see different results. Use MCP over
shelling out when the host doesn't want to spawn a subprocess per lookup,
or when it can hold a longer-lived server connection across a whole
session instead of one CLI invocation per question.

### Configuring an agent to use it

Any MCP host that can launch a stdio server works. The general shape:

```json
{
  "mcpServers": {
    "nexus": {
      "command": "nexus",
      "args": ["mcp"]
    }
  }
}
```

- **Claude Code**: `claude mcp add nexus -- nexus mcp` (add `-s project` to
  commit it to the repo's `.mcp.json` for the whole team, instead of just
  your own local config).
- **Cursor / other JSON-config hosts**: add the block above to the host's
  MCP config file (e.g. Cursor's `.cursor/mcp.json`).
- **Any other MCP-host agent**: point it at the `nexus mcp` command the
  same way — it only needs to know how to launch a stdio server.

`nexus` must be on `PATH` for the host to find it. Verify the server
responds by asking the agent to call `nexus_map`.

## The `narrate` skill

Narration requires an LLM, so it isn't built into the `nexus` binary — it's
a skill (`skills/narrate/SKILL.md`) that a coding agent runs. This repo
doubles as a plugin source for the agents that support skill/plugin
discovery:

- **Claude Code**: `/plugin marketplace add sunprema/nexus-cli`, then
  install the `nexus` plugin.
- **Codex / Cursor**: point their plugin config at this repo
  (`.codex-plugin/plugin.json`, `.cursor-plugin/plugin.json`).
- **OpenCode**: see [`.opencode/INSTALL.md`](.opencode/INSTALL.md).
- **Gemini**: `gemini-extension.json` at the repo root.

Once installed, ask your agent to "narrate this change" after making a
commit, or let it pick up `.nexus/pending.json` on its own.

The prompt/style the skill writes in is editable per-repo at
`.nexus/skills/narrator-prompt.md` (created by `nexus init`) — tune tone,
vocabulary, or conventions without a CLI release.

## Editor integration

[nexus-vscode](https://github.com/sunprema/nexus-vscode) is a VS Code
extension for reading explainer entries without leaving the editor: a
CodeLens on every file shows its narration status, and clicking it opens
the explainer split-screen beside the code. It shells out to this CLI
(`nexus show`/`diff`/`map`/`tour`), so it needs `nexus` on `PATH`.

## Development

```bash
go build ./...
go vet ./...
go test ./...
golangci-lint run ./...
```

CI (`.github/workflows/ci.yml`) runs the same checks on Linux, macOS, and
Windows. Releases are built with [goreleaser](https://goreleaser.com) on
tag push (`.github/workflows/release.yml`).

## License

MIT — see [LICENSE](LICENSE).
