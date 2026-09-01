# Explainer viewer

A static, single-page reader for any repo's `explainer` branch: paste
`owner/repo`, get every narrated file with its summary, the narrative
beside the code, and the guided tours — in a browser, with nothing
installed.

It exists so a Nexus repo can be *shown* to someone from a link. The VS
Code extension ([nexus-vscode](https://github.com/sunprema/nexus-vscode))
is still the way to read explainer entries while you work; this is the way
to read them when you have a URL and no setup.

Deployed to GitHub Pages by `.github/workflows/pages.yml` on every push to
`main` that touches this directory. Pages must be set to the "GitHub
Actions" source once, under Settings → Pages.

## URL parameters

Every piece of state lives in the query string, so any view is a shareable
link (the **Copy link** button copies the current one):

| Parameter | Meaning | Default |
| --- | --- | --- |
| `repo` | `owner/repo`, or any GitHub URL for it | `sunprema/nexus-cli` |
| `file` | Code path to open, e.g. `internal/cli/show.go` | — |
| `line` | Line to highlight in the code pane | — |
| `tour` | Tour slug to start | — |
| `stop` | 1-based stop within that tour | `1` |
| `view` | `narrative`, `split`, or `code` | `narrative` |
| `branch` | Explainer branch name | `explainer` |
| `code` | Ref to read code from | `HEAD` (the default branch) |

Example: `?repo=sunprema/nexus-cli&file=internal/cli/show.go&view=split`

## How it reads a repo

No server, no build, no API token. Two GitHub endpoints:

- **The REST tree API, once per repo load** — the only way to list a
  branch's files. Unauthenticated it is capped at 60 requests/hour per IP,
  which is why it is used exactly once and never per file. Hitting the cap
  shows a dated message rather than an empty page.
- **`raw.githubusercontent.com` for every blob** — not rate-limited, so
  browsing stays free however many files a visitor opens. Explainer entries
  come from the `explainer` branch, code from `HEAD`.

Only public repositories work: both endpoints are unauthenticated.

The three Nexus conventions it depends on are owned by the CLI
(`internal/cli/show.go`, `tour.go`, `frontmatter.go`) and hand-mirrored in
`viewer.js`, the same way `nexus-vscode`'s `cliClient.ts` mirrors the
`--json` shapes:

- an entry for `<path>` lives at `<path>.md` on the explainer branch,
- tours live under `.nexus/tours/`, history under `.nexus/history/`,
- YAML frontmatter carries `summary`, `source_commit`, `desynced`, `tests`,
  and frontmatter wins over the `> **Nexus desync**` marker scan when both
  are present.

Change any of those in the CLI and this file needs the same change.

## Local development

```bash
python3 -m http.server 8099 --directory docs
# then open http://localhost:8099
```

There is no toolchain — plain HTML, CSS, and one ES module.

## Vendored dependencies

`vendor/` holds pinned copies of four libraries rather than loading them
from a CDN: the page then makes no third-party requests at all, keeps
working if a CDN is down or blocked, and can't silently change under a
visitor. Refresh them deliberately:

```bash
npm pack marked@15.0.7 dompurify@3.2.4 js-yaml@4.1.0 @highlightjs/cdn-assets@11.11.1
# extract, then copy in as vendor/<name>-<version>.min.js and update index.html
```

| File | Package | Why |
| --- | --- | --- |
| `marked-15.0.7.min.js` | marked | Markdown → HTML |
| `purify-3.2.4.min.js` | DOMPurify | sanitizes that HTML — explainer content is arbitrary Markdown from whatever repo the visitor typed |
| `js-yaml-4.1.0.min.js` | js-yaml | frontmatter, parsed the same way the CLI parses it |
| `highlight-11.11.1.min.js` + `highlight-github-*.min.css` | highlight.js | syntax highlighting in the code pane |

## Not built yet

- **`.nexus/history/`** — incidents, decisions, and reverts are skipped in
  the file list; showing them on a file would mean reading every history
  record up front.
- **`nexus diff`** — the explainer's own change history needs a second tree
  walk per file (or the commits API, which is rate-limited).
- **Private repos** — would need an OAuth token, which a static page can't
  hold safely.
