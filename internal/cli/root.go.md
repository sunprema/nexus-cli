---
path: "internal/cli/root.go"
summary: "Builds the flattened `nexus` root command and wires in every subcommand."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/root.go

## What this does
`NewRootCmd` constructs the top-level `nexus` Cobra command and attaches
every subcommand: `init`, `sync`, `narrated`, `check`, `show`, `diff`,
`map`, `tour`, and the hidden `__post-commit-hook`. This is the single
place the CLI's shape is assembled.

## How it works
Nothing clever — it's a flat list of `cmd.AddCommand(...)` calls, one per
subcommand constructor. `SilenceErrors: true` is set on the root command so
Cobra never prints an error itself; `main.go` owns error output instead
(see its own explainer entry for why). This root command replaces what used
to be a subcommand group (`entire nexus ...`) nested under a much larger
CLI — here it *is* the whole program, so command names read as `nexus init`
rather than `nexus nexus init`.

## Recent changes
- Initial port: this file replaces entireio/cli's `newNexusGroupCmd` (which added a "nexus" group under the `entire` root) with a standalone root command (37415bf)
