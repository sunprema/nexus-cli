---
path: "internal/cli/show.go"
summary: "The `nexus show <path>` command: prints one code file's current explainer entry, straight from the branch's git objects."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
desynced: false
---

# internal/cli/show.go

## What this does
`nexus show <path>` looks up `<path>.md` on the tip of the `explainer`
branch and prints it — no checkout, no worktree, just a direct git-object
read. `--json` additionally surfaces the frontmatter fields (summary,
source commit, desync status, and a test file's per-test intent list) as
separate structured fields, rather than leaving a caller to parse them out
of the raw content itself.

## How it works
The real logic lives in `computeNexusShow`, kept separate from the Cobra
command so the same lookup can eventually back other surfaces (an IDE
extension, for instance) without duplicating branch-name resolution or
path-mapping. Every "normal" outcome — Nexus not set up, the explainer
branch missing, no entry yet, an entry found (desynced or not) — is
reported through the returned result struct, never as a Go error; an
actual error return means something genuinely went wrong, like the repo
failing to open.

## Recent changes
- Initial port from entireio/cli's nexus_show.go, unchanged apart from import paths, the "nexus show"/"nexus init" message wording, and the doc comment's example shell-out command (37415bf)
