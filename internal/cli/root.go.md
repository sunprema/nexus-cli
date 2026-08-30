---
path: "internal/cli/root.go"
summary: "Builds the flattened `nexus` root command, wires in every subcommand (including `speak`), and sets the version Cobra reports for --version."
desynced: false
---

# internal/cli/root.go

## What this does
`NewRootCmd` constructs the top-level `nexus` Cobra command and attaches
every subcommand: `init`, `sync`, `narrated`, `check`, `show`, `diff`,
`map`, `tour`, `speak`, the hidden `__post-commit-hook`, and the hidden
`mcp` (MCP stdio server). This is the single place the CLI's shape is
assembled. It also sets Cobra's `Version` field from
`versioninfo.Version`, which is what makes `nexus --version` work.

## How it works
Nothing clever — it's a flat list of `cmd.AddCommand(...)` calls, one per
subcommand constructor. `SilenceErrors: true` is set on the root command so
Cobra never prints an error itself; `main.go` owns error output instead
(see its own explainer entry for why). This root command replaces what used
to be a subcommand group (`entire nexus ...`) nested under a much larger
CLI — here it *is* the whole program, so command names read as `nexus init`
rather than `nexus nexus init`. Setting `Version` is what makes Cobra
auto-register the `--version` flag; without it, the flag wouldn't exist at
all.

## Recent changes
- Wired in the `speak` subcommand (read an explainer entry aloud via the OS's text-to-speech; see `speak.go`) (pending commit)
- Wired in the `mcp` subcommand and set `Version: versioninfo.Version` (bddecdf)
- Initial port: this file replaces entireio/cli's `newNexusGroupCmd` (which added a "nexus" group under the `entire` root) with a standalone root command (37415bf)
