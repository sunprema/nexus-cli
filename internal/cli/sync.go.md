---
path: "internal/cli/sync.go"
summary: "The `nexus sync` command: reports which commits are still waiting for narration."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/sync.go

## What this does
`nexus sync` is a read-only status check: it prints every commit currently
queued in `.nexus/pending.json`, so a developer can tell at a glance
whether `explainer` has fallen behind `main`. It never performs narration
itself — that needs an LLM, which is the `narrate` skill's job, not this
command's.

## How it works
For each queued entry it shows the short SHA, how long it's been pending
(rounded to the minute, or "just now" for anything under a minute), and —
best-effort — the commit's subject line, looked up by opening the repo and
reading the commit object directly. If the repo can't be opened, or a
particular commit no longer exists locally (e.g. dropped by a rebase), it
still prints the entry, just without a subject, rather than failing the
whole command over one lookup.

## Recent changes
- Initial port from entireio/cli's nexus_sync.go, unchanged apart from import paths and the "run 'nexus init'" message wording (37415bf)
