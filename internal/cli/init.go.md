---
path: "internal/cli/init.go"
summary: "The `nexus init` command: sets up the explainer branch, .nexus/ config (including the optional verifier_model setting), and the post-commit hook, safely re-runnable."
source_commit: 6252f8836dc145b6585a4fab422e1d3c484d9ea5
desynced: false
---

# internal/cli/init.go

## What this does
This is what `nexus init` runs. It creates everything Nexus needs in a
repo: the orphan `explainer` branch, `.nexus/settings.json`,
`.nexus/skills/narrator-prompt.md` (the editable house-style prompt),
`.nexus/.gitignore`, `.nexusignore`, and the post-commit hook. It also
defines the fixed paths and constants (`nexusDir`, `nexusSettingsFile`,
`nexusExplainerBranch`, etc.) that the rest of the command surface reads
from, and the `NexusSettings` struct that describes what `settings.json`
holds: `source_of_truth` and `explainer_branch` (both recorded but fixed)
plus `verifier_model`, the one field a team is meant to edit.

## How it works
Each piece is created independently through its own `ensure*` helper, and
each reports back whether it *created* something or found it already
there — so re-running `nexus init` on an already-set-up repo is safe and
just confirms the current state rather than erroring or duplicating work.
The one interesting piece is `ensureExplainerBranch`: it builds the orphan
branch's initial commit directly through go-git's plumbing objects (an
empty tree, a commit pointing at it, a branch ref pointing at the commit)
rather than checking anything out — so the developer's current branch and
working tree are never touched, even though a whole new branch is being
created. `nexusExplainerBranch` is a constant, not user-configurable:
`main` is always the source of truth, so there's nothing to gain from
letting the branch name drift.

`verifier_model` is different in kind from the other two settings: it's
read by the `narrate` skill, not by this binary. When set, it names the
model the skill's independent verifier pass should run on; empty means
"use the coding agent's usual subagent model". It's empty by default and
`nexus init` never rewrites an existing `settings.json`, so repos set up
before the field existed keep working unchanged — they just don't have
the line until someone adds it by hand.

## Recent changes
- Added `VerifierModel` (`verifier_model`) to `NexusSettings` so a team can pin the narrate skill's verifier subagent to a specific model instead of inheriting whatever drafted the narrative; unset by default so existing repos are unaffected (6252f88)
- Initial port from entireio/cli's nexus_init.go — `checkpoint.GetGitAuthorFromRepo` and `strategy.GetHooksDir` calls were swapped for this repo's own inlined `gitAuthorFromRepo`/`gitHooksDir`, and all "entire nexus init" message text became "nexus init" (37415bf)
