---
name: narrate
description: >
  Updates the Project Nexus 'explainer' branch with a short, jargon-free
  narrative of what a code change does and why, mirroring each changed code
  file as its own Markdown file. Use after making a code change — staged,
  committed, or still in the working tree — when the user asks to "narrate
  this change", "update the explainer", "explain this for reviewers", or
  after finishing a task in a repo that has Nexus set up. Requires
  `nexus init` to have been run first.
argument-hint: [files or ref to narrate; defaults to the current change]
---

# Nexus Narrate (SkillNarrator)

Project Nexus keeps two branches in sync: `main` holds the code, `explainer`
holds a plain-language narrative of it — one Markdown file per code file,
same path, `.md` appended. Reviewers read `explainer` instead of the raw
diff. This skill is what writes that narrative.

`main` is always the source of truth. This skill never edits code to match
the explainer, and never touches the developer's current checkout — all
writes happen in a separate git worktree checked out to `explainer`.

`nexus init` installs a post-commit hook that queues every commit in
`.nexus/pending.json` for narration — cheaply, with no LLM call of its own
(see `docs/adr/0001-async-post-commit-narration-trigger.md` for why). This
skill is what drains that queue: it's the only place the actual LLM-backed
narration happens, and it's expected to run some time after the commit, not
synchronously with it. Each explainer commit this skill makes carries a
`Source-Commit: <main-sha>` trailer pointing back at the commit it narrates —
the reverse of what you might expect, and deliberately so: the explainer
commit doesn't exist yet at `main`-commit time, so linking the other way
would mean amending `main` after the fact, which breaks once a commit is
pushed.

Before committing, this skill also **verifies** each explainer entry against
the code with a second, independent pass (step 7) — catching the kind of
thing a narrator confidently gets wrong, like describing 5 retries when the
code does 3. A mismatch never blocks the commit: `main` is always the
source of truth, so a disagreement gets written into the explainer file as
a visible `**Nexus desync**` marker instead, and the commit proceeds. See
`docs/adr/0002-nonblocking-desync-markers.md`. `nexus check` reports
which files still carry one.

Not every changed file gets narrated. `.nexusignore` (repo root, gitignore
syntax, created by `nexus init`) names paths this skill skips —
ships with lockfiles and common test-file patterns pre-enabled, since a
test's own name and assertions usually already say what it checks. It's
team-editable; see `docs/adr/0003-nexusignore.md`.

A team that removes the test-file lines from `.nexusignore` gets more than
ordinary narration for those files: this skill also recognizes test files
by name (step 6) and adds a `tests` list to their frontmatter — one entry
per test function naming what it actually verifies, for the case the
default's rationale doesn't cover (an agent-written test whose name and
assertions *don't* already say what it checks). This is opt-in through the
same mechanism as narrating tests at all — no separate toggle.

Every explainer file starts with YAML frontmatter — `path`, a one-sentence
`summary` (terser than "What this does": written for someone scanning many
files' frontmatter, not reading this one file in full), `source_commit`,
and `desynced`. This is what lets a reader — human or agent — get a file's
gist without opening the full narrative, the same way a skill's own
frontmatter works. A recognized test file also carries `tests`: one
`{name, intent}` entry per test function — see step 6. `desynced` is
written in **both** places: the
frontmatter field (machine-authoritative — `nexus check`/`nexus show`
trust this over scanning prose) and the inline callout (human-visible in
the rendered document). See `docs/adr/0004-explainer-frontmatter.md` for
why both, not one or the other.

## When This Skill Activates

Use after a code change that should be reviewable via the explainer branch —
most commonly to drain `.nexus/pending.json` (commits the post-commit hook
queued), but also to narrate a change that's staged or still unstaged,
before it's even committed.

Do **not** use for whitespace-only diffs, or for a change whose files are
entirely excluded by `.nexusignore` (step 3 checks this and reports nothing
to narrate).

## Response Format

Begin the first response to this skill invocation with the line:

`Nexus Narrate:`

followed by a blank line, then the content.

- Apply the header to the **first response of the invocation only.**
- Do **not** include the header on error or early-exit responses (e.g. "Nexus
  is not set up in this repo", "nothing to narrate").

## Rules

1. Never modify anything on `main` or the developer's current branch/working
   tree. All explainer writes go through the isolated worktree in step 5.
2. `main` is always the source of truth. If the code and an existing
   explainer entry disagree, trust the code — rewrite the narrative, never
   the reverse.
3. Keep explanations short, crisp, and jargon-free. A non-specialist teammate
   should be able to follow them. Prefer 2–4 sentences per section over a
   wall of text.
4. Explain *why* the change was made and *how* the logic flows — not what
   each line does. The reviewer can already see the code diff if they want
   that.
5. Add a fenced mermaid diagram only for genuinely non-linear control flow.
   Most files don't need one.
6. Mirror the path exactly: `path/to/file.ext` → `path/to/file.ext.md` in
   the explainer branch.
7. Treat each explainer file as a living document, not a changelog: update
   "What this does" / "How it works" **and the frontmatter's `summary`** to
   reflect the file's current state, and record this change as one bullet
   under "Recent changes". Keep at most 5 bullets there, dropping the
   oldest.
8. If a code file was deleted, delete its mirrored `.md` too — no orphaned
   narration.
9. If a code file was renamed, `git mv` the mirrored `.md` before updating
   its content, so its history follows.
10. Read the repo's `.nexus/skills/narrator-prompt.md` before writing
    anything and follow it — it's the team's editable house style (tone,
    vocabulary, extra conventions). It can adjust style; it cannot override
    rules 1–2 above, which are structural.
11. `.nexus/` and `.git*` paths are never narrated, full stop — that's
    structural, not a preference, and isn't something `.nexusignore` can
    override. Everything else a team wants excluded (tests, lockfiles,
    whatever) belongs in `.nexusignore`, not as a special case added to
    this skill.
12. Verify every drafted entry (step 7) with a fresh subagent that has no
    memory of drafting it — an agent checking its own work isn't
    independent. Give the verifier only the code and the drafted text, not
    the narrator's reasoning or the house-style prompt. Run it on
    `.nexus/settings.json`'s `verifier_model` when set (step 1); otherwise
    your default subagent model.
13. A verified mismatch is never a reason to withhold the commit or edit the
    code. Write it as a `**Nexus desync**` marker in the explainer file
    (step 7) and commit anyway.
14. Every explainer file gets YAML frontmatter (`path`, `summary`,
    `source_commit`, `desynced`) — step 6 writes it, step 7 keeps
    `desynced` in sync with the inline callout. Never one without the
    other: a file with a callout but frontmatter still saying
    `desynced: false` (or vice versa) is worse than either alone, since a
    reader trusting one will be misled by the other.
15. For a recognized test file (step 6), write one `tests` entry per test
    function naming what it actually *verifies* — the scenario or
    invariant, e.g. "retries on a transient network error, not on a 4xx" —
    never a restatement of its assertions, e.g. "asserts err is nil after
    three calls". A reader can already see the assertions; the reason
    `tests` exists is to say what they're *for*.

## Process

### 1. Verify Nexus is set up

```bash
test -f .nexus/settings.json && test -f .nexus/skills/narrator-prompt.md
```

If either is missing, stop and tell the user: "Nexus isn't set up in this
repo. Run `nexus init` first." Do not print the response header.

Read `.nexus/settings.json`'s `verifier_model` field and hold onto it for
step 7 — empty (the default) means "use your normal subagent model," a
non-empty value names the model the verifier subagent should run on
instead.

### 2. Read the house style

Read `.nexus/skills/narrator-prompt.md` in full. It may adjust tone, flag
domain vocabulary to avoid, or add project-specific conventions — apply it
on top of the rules above.

### 3. Determine what to narrate

Unless the user's argument names specific files or a ref, check the pending
queue first:

```bash
cat .nexus/pending.json 2>/dev/null
```

- **Queue has entries**: narrate them **one commit at a time, oldest
  `recorded_at` first.** Steps 4–8 below describe one iteration — repeat
  the full cycle (including step 8's dequeue) for each queued commit before
  moving to step 9. Don't batch multiple commits into one explainer commit;
  each keeps its own `Source-Commit` trailer and its own "Recent changes"
  bullet.
- **Queue is empty or missing**: fall back to whichever of these is
  non-empty, most-specific first:

  ```bash
  git diff --cached --name-status   # 1. staged changes
  git diff --name-status            # 2. unstaged changes (if 1 is empty)
  ```

  This covers narrating in-progress work before it's committed. There's no
  commit to link yet in this case — no `Source-Commit` trailer, and nothing
  to dequeue in step 8.
- **Nothing applies** (empty queue, nothing staged or unstaged): tell the
  user there's nothing to narrate and stop.

From whichever file list you land on, drop:

- Paths under `.nexus/` or `.git*` — structural, never narratable,
  regardless of `.nexusignore` (see Rules).
- Any file the diff reports as binary.
- Any file `.nexusignore` excludes, checked per file with:

  ```bash
  git -c core.excludesFile=.nexusignore check-ignore --no-index -q -- <file>
  ```

  Exit 0 means excluded, skip it. This reuses git's own gitignore-pattern
  matching rather than hand-rolling glob logic — `.nexusignore` uses plain
  gitignore syntax. A missing `.nexusignore` (repo predates this feature)
  just means nothing extra gets excluded here.

### 4. Gather diff and context per file

For each remaining file, using whichever scope was selected in step 3:

- The diff for that file — `git show --name-status <sha> -- <file>` and
  `git show <sha> -- <file>` for a queued commit; `git diff --cached -- <file>`
  or `git diff -- <file>` for the staged/unstaged fallback.
- The file's current full content: `git show <sha>:<file>` for a queued
  commit, or the working tree file otherwise — the explanation should be
  grounded in the whole file, not just the changed hunk.

### 5. Set up the explainer worktree

```bash
GIT_COMMON_DIR=$(git rev-parse --git-common-dir)
WORKTREE="$GIT_COMMON_DIR/nexus/worktrees/explainer"
if ! git worktree list --porcelain | grep -qx "worktree $WORKTREE"; then
  mkdir -p "$(dirname "$WORKTREE")"
  git worktree add "$WORKTREE" explainer 2>/dev/null || {
    git worktree prune
    git worktree add "$WORKTREE" explainer
  }
fi
```

This is a second, independent working directory pointed at `explainer`. It
never checks out or modifies the developer's current branch.

If `git worktree add` still fails after pruning (e.g. the `explainer`
branch itself doesn't exist), stop and tell the user to run `nexus init`.

### 6. Write or update each explainer file

For each in-scope code file `path/to/file.ext`, target
`$WORKTREE/path/to/file.ext.md`:

- **Deleted** code file: `git rm -f path/to/file.ext.md` inside the
  worktree if it exists, then move on.
- **Renamed** from `old.ext` to `new.ext`: inside the worktree,
  `git mv old.ext.md new.ext.md` if the old one exists; otherwise treat as
  a new file at the new path.
- Otherwise, read the existing `path/to/file.ext.md` if present (for
  continuity — reuse its framing and pull its existing "Recent changes"
  bullets), then write:

```markdown
---
path: "path/to/file.ext"
summary: "<one sentence, terser than \"What this does\">"
source_commit: <full 40-character sha>
desynced: false
---

# path/to/file.ext

## What this does
<2-4 sentences: the file's purpose, current state>

## How it works
<short logic-flow explanation; a fenced mermaid diagram only if the control
flow genuinely needs one>

## Recent changes
- <this change: what shifted and why, one sentence> (<short ref>)
<up to 4 more bullets carried over from before, oldest dropped>
```

**Test files**: after writing the template above, check whether
`path/to/file.ext` matches a common test-file naming convention —
`*_test.go`, `*.test.{ts,tsx,js,jsx}`, `*_test.py`, `test_*.py`,
`*_spec.rb`, `*.spec.{ts,js}`, or a path under `test/`, `tests/`,
`__tests__/`, or `spec/`. If it does, add a `tests` field to the
frontmatter (rule 15):

```yaml
tests:
  - name: <test function's own name, e.g. TestRetry_TransientError>
    intent: "<one sentence: what this test actually verifies>"
```

One entry per test function in the file, in the order they appear. Read
each test function's body, not just its name — the intent is what the
test covers, which the name alone often doesn't fully say (and can't be
trusted to say at all when the test itself was agent-written). This is
additive to the template above, not a replacement for it — a test file
still gets the same "What this does" / "How it works" / "Recent changes"
sections as any other file.

This only runs for files that reached this step at all — a file
`.nexusignore` excludes never gets here, tests included. A team gets this
the same way it gets any test narration: remove the test-file lines from
`.nexusignore`.

Notes on the frontmatter:

- **Always double-quote `path` and `summary`, and escape any `"` inside
  them as `\"`.** A summary is free text; if it happens to contain
  `: ` (colon-space) unquoted — easy to hit in an ordinary sentence, e.g.
  "Implements X, which does Y: the details" — YAML reads that as a nested
  mapping and fails to parse. `parseNexusFrontmatter` treats a parse
  failure as "no frontmatter at all," so this doesn't crash anything — it
  silently drops the whole frontmatter block, which is worse: the failure
  is invisible until something is missing. Quoting always is cheaper than
  auditing every summary for colons.
- `summary` is one sentence, written for someone scanning many files'
  frontmatter, not reading this one file in full — terser than "What this
  does". Never carry forward an old `summary` unexamined: regenerate it
  fresh from the current file, same as "What this does".
- `source_commit` is the full 40-character SHA when scope was a queued
  commit. Omit the line entirely for the staged/unstaged fallback — there's
  no commit yet to name.
- Write `desynced: false` here regardless of what the previous version
  said — step 7, which always runs right after this step for the same
  file, is what re-evaluates and sets it.
- Quote `name` and `intent` in each `tests` entry the same way as `path`
  and `summary` above, and for the same reason — an unquoted `intent`
  containing `: ` fails to parse the same way an unquoted `summary` does.

`<short ref>` is the queued commit's short SHA, or `pending commit` for the
staged/unstaged fallback.

Create parent directories under the worktree as needed.

### 7. Verify against the code

For each file just written in step 6 (skip ones that were only deleted —
nothing to verify), spawn a **fresh subagent with no memory of drafting
it** — verifying your own draft isn't independent. If step 1's
`verifier_model` is non-empty, run this subagent on that model instead of
your default; otherwise use your default. Give it only:

- The code: the diff and full file content gathered in step 4.
- The explainer content just written in step 6.

Ask it one thing: do the explainer's factual claims match the code? It
should report concrete mismatches only — counts, conditions, order of
operations — not style or completeness opinions. If the frontmatter has a
`tests` list, each entry's `intent` is a factual claim too: it should
match what that test function actually verifies, not just sound
plausible.

- **Mismatch found**: set the frontmatter's `desynced: true`, **and** add
  this as the first section after the file's `#` title, before "What this
  does" (replacing any existing one from a prior run):

  ```markdown
  > [!WARNING]
  > **Nexus desync** — <one-line mismatch, e.g. "explainer says 5 retries;
  > code retries 3">
  ```

  Both together, always — `nexus check`/`nexus show` trust the frontmatter
  field, but a human reading the rendered file only sees the callout. One
  without the other means whichever reader trusts the missing half gets
  misled. See rule 14.
- **No mismatch, but a marker (frontmatter `desynced: true`, or an inline
  callout, or both) from a previous run is present**: clear both — set
  `desynced: false` and remove the callout. It's resolved.
- **No mismatch and no existing marker**: nothing to do (frontmatter
  should already read `desynced: false` from step 6).
- **Verifier itself fails to run** (subagent error, no model available):
  proceed without changing either the frontmatter field or the callout,
  and note it in the report (step 9) — an unreachable verifier is not
  evidence of a mismatch.

### 8. Commit the explainer branch, and dequeue

```bash
cd "$WORKTREE"
git add -A
git status --porcelain   # if empty, nothing to commit — skip and say so
```

Commit message depends on which scope step 3 selected:

- **Narrating a queued commit** (full 40-character `<sha>` from the queue
  entry):

  ```bash
  git commit -m "Narrate: <one-line summary of the change>

  Source-Commit: <sha>"
  ```

  Then, back in the original repo directory (not the worktree), dequeue it:

  ```bash
  nexus narrated <sha>
  ```

- **Staged/unstaged fallback** (no commit to link yet): commit without a
  trailer, and skip the dequeue step — there's nothing queued to remove.

  ```bash
  git commit -m "Narrate: <one-line summary of the change>"
  ```

If step 3 selected multiple queued commits, repeat steps 4–8 for each one,
in order, before moving to step 9.

### 9. Report

Print the `Nexus Narrate:` header, then group by commit (or the single
staged/unstaged pass): for each, list the files touched with a one-line
summary each (or "deleted — code file removed"), flagging any file where
step 7 added a `**Nexus desync**` marker, and the explainer commit it
landed in. If step 8 was skipped anywhere, say "No explainer changes were
needed" for that entry instead. If the queue started with more entries than
you finished, say how many remain (e.g. "3 of 5 pending commits narrated —
stopped after a failure on <sha>, see below").

## Failure Modes

- **Nexus not set up**: tell the user to run `nexus init`; stop.
- **Nothing changed**: say so; do not fabricate a narrative.
- **Worktree can't be created** (e.g. `explainer` branch missing): tell the
  user to run `nexus init`; stop.
- **A file is binary or not valid text**: skip narrating it and note that in
  the report; don't fail the whole run over one file.
- **A queued commit no longer exists locally** (e.g. `.nexus/pending.json`
  outlived a rebase that dropped it): note it in the report, still dequeue
  it with `nexus narrated <sha>` (nothing left to narrate), and move
  on to the next queued commit.
- **The verifier subagent fails to run**: proceed without adding or
  removing a desync marker for that file, note it in the report, and don't
  fail the whole run over it.
