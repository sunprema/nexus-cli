---
path: "internal/cli/init.go"
summary: "The `nexus init` command: sets up the explainer branch, .nexus/ config, and the post-commit hook, safely re-runnable."
source_commit: 37415bfd4a285d146e57efae57d521273423b5bb
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
from.

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

## Recent changes
- Initial port from entireio/cli's nexus_init.go — `checkpoint.GetGitAuthorFromRepo` and `strategy.GetHooksDir` calls were swapped for this repo's own inlined `gitAuthorFromRepo`/`gitHooksDir`, and all "entire nexus init" message text became "nexus init" (37415bf)
