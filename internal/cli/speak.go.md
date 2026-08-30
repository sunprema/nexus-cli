---
path: "internal/cli/speak.go"
summary: "The `nexus speak` command and the engine behind the nexus_speak MCP tool: reads an explainer entry aloud through the operating system's own text-to-speech, with one-speaker-at-a-time stop/replace."
desynced: false
---

# internal/cli/speak.go

## What this does
`nexus speak <path>` reads a code file's explainer entry out loud —
the whole narrative by default, or just the one-sentence summary with
`--summary`. It uses whatever text-to-speech the operating system already
has (`say` on macOS; `espeak-ng`, `espeak`, or `spd-say` on Linux, tried
in that order; Windows' built-in `System.Speech` through PowerShell), so
nothing extra needs installing and it works from a plain terminal. The
same code serves the `nexus_speak` MCP tool, so an agent can do "read me
the summary of auth.py" from any editor. `--text "..."` (or `-` for
stdin) reads arbitrary text instead; `--print` shows what would be read
without saying it; `--stop` interrupts whatever is playing.

## How it works
A request (`nexusSpeakRequest`) is built the same way by the CLI flags
and by the MCP tool, then goes through `runNexusSpeak`:

1. **Stop?** If asked to stop, kill the current speaker and return —
   nothing is read.
2. **Get the text.** For a path, look up the entry with the same
   `computeNexusShow` that `nexus show` uses, then take either the
   frontmatter summary or the narrative body (frontmatter removed). "No
   entry yet", "no summary yet", "Nexus not set up" and "not a git repo"
   come back as an ordinary result with an `error` message, not a
   failure. Free `text` skips the lookup entirely. An unknown `mode` is a
   real error.
3. **Make it speakable.** `nexusSpeakableText` turns Markdown into prose
   an engine can read: code fences and mermaid blocks are dropped;
   headings, bullets and numbered items lose their markers and gain a
   trailing period so the voice pauses; links, images, emphasis,
   backticks and HTML tags are unwrapped; table rows become
   comma-separated lists; blank-line runs collapse. Desync marker lines
   are removed — and because a listener can't see the callout, a
   desynced entry is instead prefixed with the spoken sentence "Heads
   up: this explainer may be out of date with the code." (in both
   modes). If nothing is left after stripping, that's another ordinary
   "nothing to speak" result.
4. **Report or speak.** The result always carries a word count, a rough
   duration estimate (about 2.6 words per second), and a short preview.
   With `--print` the full prepared text is returned and no engine is
   needed at all. Otherwise the first installed engine is picked (a
   missing engine is a real error naming what to install), any current
   speaker is stopped so voices don't overlap, and the text is piped to
   the engine over stdin — never as a command-line argument, so a long
   narrative can't hit an argument-length limit. The voice comes from
   `--voice`, else the `NEXUS_SPEAK_VOICE` environment variable, else the
   engine's default; it's an environment variable rather than a
   `settings.json` field because it's a personal preference, not a
   team one.

The CLI **blocks** until the engine finishes, so Ctrl-C behaves as a
person expects. The MCP tool runs **detached**: the process is started,
the whole text is written to its stdin and stdin is closed *before*
returning (so a short-lived host can't truncate the audio mid-sentence),
and a background goroutine reaps the process when it ends — its exit
status is discarded, since the call has already returned. In blocking
mode a signal-killed engine — the normal way an utterance ends early when
something else says stop — is not reported as an error; only a genuine
non-zero exit is.

**One speaker per machine.** The running engine's pid and name are
written to `nexus-speak.pid` in the OS temp directory (not the repo,
since the speaker is per machine). A stop request from any process — the
MCP server, a second shell — reads that file, and on Unix first checks
via `ps` that the pid is still running the recorded engine, so a stale
record whose pid has since been reused by something unrelated is
discarded rather than killed (Windows has no `ps`; it trusts the
record). When an utterance finishes, the file is removed only if it
still names that pid, so a finished speaker never erases the record of
a newer one that replaced it.

## Recent changes
- New file: `nexus speak` command plus the shared `runNexusSpeak` used by the `nexus_speak` MCP tool — native OS text-to-speech chosen over the browser Web Speech API so it works from a terminal and from every MCP host with no extension (see ADR 0005) (pending commit)
