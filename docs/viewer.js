/**
 * Nexus Explainer Viewer — reads any repo's `explainer` branch straight
 * from GitHub, in the browser, with no install and no server.
 *
 * The only thing this needs to know about Nexus is the convention the CLI
 * owns (see internal/cli/show.go): an explainer entry for `<path>` lives at
 * `<path>.md` on the `explainer` branch, tours live under `.nexus/tours/`,
 * and the YAML frontmatter carries summary / source_commit / desynced.
 * Keep those three constants in sync with the CLI by hand — the same
 * hand-sync the VS Code extension's cliClient.ts documents.
 *
 * Two GitHub endpoints, deliberately: the REST tree API *once* per repo
 * load (it is the only way to list a branch's files, and unauthenticated
 * it is rate-limited to 60/hr per IP), then raw.githubusercontent.com for
 * every blob — which is not rate-limited, so browsing stays free no matter
 * how many files a visitor opens.
 */

const TOUR_DIR = ".nexus/tours/";
const HISTORY_DIR = ".nexus/history/";
const DESYNC_MARKER = "> **Nexus desync**";

const DEFAULT_REPO = "sunprema/nexus-cli";
const DEFAULT_EXPLAINER_BRANCH = "explainer";
/** raw.githubusercontent resolves HEAD to the repo's default branch, so the
 *  code side needs no extra API call to discover whether it is main/master. */
const DEFAULT_CODE_REF = "HEAD";

const FRONTMATTER = /^---\n([\s\S]*?)\n---\n?([\s\S]*)$/;

const $ = (id) => document.getElementById(id);
const el = {
  layout: $("layout"),
  sidebar: $("sidebar"),
  sidebarToggle: $("sidebar-toggle"),
  repoForm: $("repo-form"),
  repoInput: $("repo-input"),
  repoMeta: $("repo-meta"),
  filter: $("filter"),
  tree: $("tree"),
  panes: $("panes"),
  tourbar: $("tourbar"),
  narrativeTitle: $("narrative-title"),
  narrativeLinks: $("narrative-links"),
  narrativeBody: $("narrative-body"),
  codeTitle: $("code-title"),
  codeLinks: $("code-links"),
  codeBody: $("code-body"),
  copyLink: $("copy-link"),
  themeToggle: $("theme-toggle"),
  toast: $("toast"),
};

const state = {
  owner: "",
  repo: "",
  explainerBranch: DEFAULT_EXPLAINER_BRANCH,
  codeRef: DEFAULT_CODE_REF,
  /** Narrated code files: { path, explainerPath, summary, desynced, loaded } */
  entries: [],
  /** Guided tours: { slug, explainerPath, title, stops } */
  tours: [],
  file: null,
  line: null,
  view: "narrative",
  tour: null, // { slug, title, stops, index }
  filter: "",
};

const textCache = new Map();

/* ------------------------------------------------------------------ *
 * Small helpers
 * ------------------------------------------------------------------ */

function escapeHtml(s) {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function toast(message) {
  el.toast.textContent = message;
  el.toast.hidden = false;
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => { el.toast.hidden = true; }, 2400);
}

/** Accepts "owner/repo", a github.com/github.dev URL, or a clone URL. */
function parseRepoInput(raw) {
  const value = (raw || "").trim().replace(/\.git$/, "");
  if (!value) return null;
  const withoutScheme = value.replace(/^[a-z-]+:\/\//i, "").replace(/^git@github\.com:/i, "");
  const withoutHost = withoutScheme.replace(/^(www\.)?(github\.com|github\.dev|raw\.githubusercontent\.com)\//i, "");
  const parts = withoutHost.split("/").filter(Boolean);
  if (parts.length < 2) return null;
  const [owner, repo] = parts;
  if (!/^[\w.-]+$/.test(owner) || !/^[\w.-]+$/.test(repo)) return null;
  return { owner, repo };
}

async function fetchText(url) {
  if (textCache.has(url)) return textCache.get(url);
  const promise = fetch(url).then((res) => (res.ok ? res.text() : Promise.reject(new HttpError(res.status, url))));
  textCache.set(url, promise);
  promise.catch(() => textCache.delete(url)); // never cache a failure
  return promise;
}

class HttpError extends Error {
  constructor(status, url) {
    super(`${status} for ${url}`);
    this.status = status;
  }
}

const rawUrl = (ref, path) =>
  `https://raw.githubusercontent.com/${state.owner}/${state.repo}/${encodeURIComponent(ref)}/${path.split("/").map(encodeURIComponent).join("/")}`;

/**
 * Splits an explainer file into frontmatter and body, mirroring the CLI's
 * parseNexusFrontmatter: bad or absent YAML degrades to "no frontmatter,
 * whole file is the body" rather than an error.
 */
function parseFrontmatter(content) {
  const normalized = content.replace(/\r\n/g, "\n");
  const match = FRONTMATTER.exec(normalized);
  if (!match) return { fm: {}, body: normalized, hasFrontmatter: false };
  try {
    const fm = window.jsyaml.load(match[1]);
    if (!fm || typeof fm !== "object") return { fm: {}, body: normalized, hasFrontmatter: false };
    return { fm, body: match[2], hasFrontmatter: true };
  } catch {
    return { fm: {}, body: normalized, hasFrontmatter: false };
  }
}

/** Frontmatter wins when present; fall back to the marker scan for files
 *  narrated before frontmatter existed. Same precedence as show.go. */
function isDesynced(content, fm, hasFrontmatter) {
  if (hasFrontmatter) return Boolean(fm.desynced);
  return content.split("\n").some((line) => line.startsWith(DESYNC_MARKER));
}

/* ------------------------------------------------------------------ *
 * Markdown
 * ------------------------------------------------------------------ */

window.marked.setOptions({ gfm: true, breaks: false });

function renderMarkdown(md) {
  const html = window.DOMPurify.sanitize(window.marked.parse(md), { USE_PROFILES: { html: true } });
  const holder = document.createElement("div");
  holder.className = "prose";
  holder.innerHTML = html;
  upgradeAlerts(holder);
  holder.querySelectorAll("a[href]").forEach((a) => {
    a.target = "_blank";
    a.rel = "noopener noreferrer";
  });
  holder.querySelectorAll("pre code").forEach((block) => window.hljs.highlightElement(block));
  return holder;
}

/** Turns GitHub's `> [!WARNING]` blockquote alerts into styled callouts —
 *  the shape every Nexus desync marker is written in. */
function upgradeAlerts(root) {
  root.querySelectorAll("blockquote").forEach((quote) => {
    const first = quote.firstElementChild;
    if (!first) return;
    const match = /^\s*\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*/i.exec(first.textContent || "");
    if (!match) return;
    const kind = match[1].toLowerCase();
    first.textContent = (first.textContent || "").slice(match[0].length);
    const callout = document.createElement("div");
    callout.className = `callout callout-${kind}`;
    const title = document.createElement("div");
    title.className = "callout-title";
    title.textContent = `${{ warning: "⚠️", caution: "🛑", note: "ℹ️", tip: "💡", important: "❗" }[kind]} ${kind.toUpperCase()}`;
    callout.append(title, ...quote.childNodes);
    quote.replaceWith(callout);
  });
}

/* ------------------------------------------------------------------ *
 * Repo loading
 * ------------------------------------------------------------------ */

async function loadRepo() {
  el.tree.innerHTML = "";
  el.repoMeta.innerHTML = `<strong>${escapeHtml(state.owner)}/${escapeHtml(state.repo)}</strong><span>loading…</span>`;
  showSkeleton();

  const api = `https://api.github.com/repos/${state.owner}/${state.repo}/git/trees/${encodeURIComponent(state.explainerBranch)}?recursive=1`;
  let tree;
  try {
    const res = await fetch(api, { headers: { Accept: "application/vnd.github+json" } });
    if (!res.ok) return showTreeError(res);
    tree = await res.json();
  } catch (err) {
    return showState("Couldn't reach GitHub", `<p>${escapeHtml(String(err))}</p>`);
  }

  const blobs = (tree.tree || []).filter((node) => node.type === "blob" && node.path.endsWith(".md"));
  state.entries = [];
  state.tours = [];
  for (const blob of blobs) {
    if (blob.path.startsWith(HISTORY_DIR)) continue;
    if (blob.path.startsWith(TOUR_DIR)) {
      state.tours.push({ slug: blob.path.slice(TOUR_DIR.length, -3), explainerPath: blob.path, title: null, stops: null });
    } else {
      state.entries.push({ path: blob.path.slice(0, -3), explainerPath: blob.path, summary: null, desynced: false, loaded: false });
    }
  }
  state.entries.sort((a, b) => a.path.localeCompare(b.path));
  state.tours.sort((a, b) => a.slug.localeCompare(b.slug));

  if (!state.entries.length && !state.tours.length) {
    renderRepoMeta();
    return showState(
      "Nothing narrated yet",
      `<p>The <code>${escapeHtml(state.explainerBranch)}</code> branch exists but holds no explainer entries.</p>`
    );
  }

  renderRepoMeta();
  renderTree();
  loadMetadata();
  loadTourTitles();
  return applyRoute();
}

function showTreeError(res) {
  if (res.status === 403 || res.status === 429) {
    const reset = Number(res.headers.get("x-ratelimit-reset"));
    const when = reset ? new Date(reset * 1000).toLocaleTimeString() : "shortly";
    return showState(
      "GitHub rate limit reached",
      `<p>Unauthenticated GitHub API calls are capped at 60/hour per IP. The limit resets at <strong>${escapeHtml(when)}</strong>.</p>
       <p>Only the file listing uses that API — reload then and browsing works again.</p>`
    );
  }
  if (res.status === 404) {
    return showState(
      "No explainer branch here",
      `<p><code>${escapeHtml(state.owner)}/${escapeHtml(state.repo)}</code> has no <code>${escapeHtml(state.explainerBranch)}</code> branch —
       or the repository is private or doesn't exist.</p>
       <p>A repo gets one by running <code>nexus init</code> and narrating a commit.</p>
       <div class="examples"><a class="chip" href="?repo=${encodeURIComponent(DEFAULT_REPO)}">Try ${escapeHtml(DEFAULT_REPO)}</a></div>`
    );
  }
  return showState("Couldn't load the explainer branch", `<p>GitHub answered <code>${res.status}</code>.</p>`);
}

/** Fills in each entry's summary and desync flag from its frontmatter.
 *  Runs against raw.githubusercontent (no rate limit) with a small
 *  concurrency cap, and paints rows as they land. */
async function loadMetadata() {
  const queue = [...state.entries];
  const token = ++loadMetadata.generation;
  const worker = async () => {
    while (queue.length) {
      if (token !== loadMetadata.generation) return; // a newer repo load owns the UI
      const entry = queue.shift();
      try {
        const content = await fetchText(rawUrl(state.explainerBranch, entry.explainerPath));
        const { fm, hasFrontmatter } = parseFrontmatter(content);
        entry.summary = typeof fm.summary === "string" ? fm.summary : "";
        entry.desynced = isDesynced(content, fm, hasFrontmatter);
      } catch {
        entry.summary = "";
      }
      entry.loaded = true;
      if (token === loadMetadata.generation) paintEntryRow(entry);
    }
  };
  await Promise.all(Array.from({ length: 6 }, worker));
  if (token === loadMetadata.generation) renderRepoMeta();
}
loadMetadata.generation = 0;

async function loadTourTitles() {
  await Promise.all(
    state.tours.map(async (tour) => {
      try {
        const { fm } = parseFrontmatter(await fetchText(rawUrl(state.explainerBranch, tour.explainerPath)));
        tour.title = typeof fm.title === "string" ? fm.title : tour.slug;
        tour.stops = Array.isArray(fm.stops) ? fm.stops : [];
      } catch {
        tour.title = tour.slug;
        tour.stops = [];
      }
    })
  );
  renderTree();
  // The welcome panel lists tours by title, and titles only exist once the
  // fetches above land — repaint it if the visitor is still sitting there.
  if (!state.file && !state.tour) showWelcome();
}

/* ------------------------------------------------------------------ *
 * Sidebar
 * ------------------------------------------------------------------ */

function renderRepoMeta() {
  const desynced = state.entries.filter((e) => e.desynced).length;
  el.repoMeta.innerHTML = `
    <strong>${escapeHtml(state.owner)}/${escapeHtml(state.repo)}</strong>
    <div class="branches">
      <span class="chip" title="Explainer branch">📖 ${escapeHtml(state.explainerBranch)}</span>
      <span class="chip" title="Narrated files">${state.entries.length} narrated</span>
      ${desynced ? `<span class="chip desync" title="Flagged with a desync marker">⚠️ ${desynced} desynced</span>` : ""}
    </div>`;
}

function matchesFilter(entry) {
  if (!state.filter) return true;
  const needle = state.filter.toLowerCase();
  return entry.path.toLowerCase().includes(needle) || (entry.summary || "").toLowerCase().includes(needle);
}

function renderTree() {
  el.tree.innerHTML = "";

  const tours = state.tours.filter((t) => !state.filter || (t.title || t.slug).toLowerCase().includes(state.filter.toLowerCase()));
  if (tours.length) {
    const group = document.createElement("div");
    group.className = "tree-group";
    group.innerHTML = "<h3>Guided tours</h3>";
    for (const tour of tours) {
      const button = document.createElement("button");
      button.className = "entry";
      button.type = "button";
      button.setAttribute("aria-current", state.tour?.slug === tour.slug ? "true" : "false");
      button.innerHTML = `<span class="entry-name">🧭 ${escapeHtml(tour.title || tour.slug)}</span>
        <span class="entry-summary">${tour.stops ? `${tour.stops.length} stop${tour.stops.length === 1 ? "" : "s"}` : "…"}</span>`;
      button.addEventListener("click", () => startTour(tour.slug));
      group.append(button);
    }
    el.tree.append(group);
  }

  const visible = state.entries.filter(matchesFilter);
  const group = document.createElement("div");
  group.className = "tree-group";
  group.innerHTML = `<h3>Narrated files${state.filter ? ` (${visible.length})` : ""}</h3>`;

  let lastDir = null;
  for (const entry of visible) {
    const dir = entry.path.includes("/") ? entry.path.slice(0, entry.path.lastIndexOf("/")) : "";
    if (dir !== lastDir) {
      const heading = document.createElement("div");
      heading.className = "tree-dir";
      heading.textContent = dir || "./";
      group.append(heading);
      lastDir = dir;
    }
    group.append(entryRow(entry));
  }
  if (!visible.length) {
    const empty = document.createElement("div");
    empty.className = "tree-dir";
    empty.textContent = "No file matches that filter.";
    group.append(empty);
  }
  el.tree.append(group);
}

function entryRow(entry) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "entry";
  button.dataset.path = entry.path;
  button.addEventListener("click", () => selectFile(entry.path));
  paintEntryRow(entry, button);
  return button;
}

function paintEntryRow(entry, node) {
  const button = node || el.tree.querySelector(`.entry[data-path="${CSS.escape(entry.path)}"]`);
  if (!button) return;
  const name = entry.path.slice(entry.path.lastIndexOf("/") + 1);
  button.setAttribute("aria-current", state.file === entry.path ? "true" : "false");
  button.innerHTML = `<span class="entry-name">${escapeHtml(name)}${entry.desynced ? '<span class="flag" title="Desync flagged">⚠️</span>' : ""}</span>
    ${entry.summary ? `<span class="entry-summary">${escapeHtml(entry.summary)}</span>` : ""}`;
}

/* ------------------------------------------------------------------ *
 * Panes
 * ------------------------------------------------------------------ */

function showSkeleton() {
  el.narrativeBody.innerHTML = `<div class="skeleton">${"<div></div>".repeat(7)}</div>`;
  el.codeBody.innerHTML = "";
}

function showState(title, bodyHtml) {
  el.narrativeBody.innerHTML = `<div class="state"><h2>${escapeHtml(title)}</h2>${bodyHtml}</div>`;
  el.codeBody.innerHTML = "";
  el.narrativeTitle.textContent = "Explainer";
  el.narrativeLinks.innerHTML = "";
  el.codeTitle.textContent = "Code";
  el.codeLinks.innerHTML = "";
}

function showWelcome() {
  const desynced = state.entries.filter((e) => e.desynced).length;
  const tourList = state.tours.length
    ? `<p>Or take a guided tour: ${state.tours
        .map((t) => `<a href="?${paramsFor({ tour: t.slug })}">${escapeHtml(t.title || t.slug)}</a>`)
        .join(", ")}.</p>`
    : "";
  showState(
    `${state.owner}/${state.repo}`,
    `<p><strong>${state.entries.length}</strong> file${state.entries.length === 1 ? "" : "s"} narrated on
      <code>${escapeHtml(state.explainerBranch)}</code>${desynced ? `, <strong>${desynced}</strong> flagged as desynced` : ""}.</p>
     <p>Pick a file to read what it is <em>for</em> — the intent behind it — beside the code itself.</p>
     ${tourList}`
  );
}

async function renderNarrative(path) {
  const entry = state.entries.find((e) => e.path === path);
  el.narrativeTitle.textContent = `${path}.md`;
  el.narrativeLinks.innerHTML = entry
    ? `<a href="https://github.com/${state.owner}/${state.repo}/blob/${encodeURIComponent(state.explainerBranch)}/${entry.explainerPath}" target="_blank" rel="noopener">On GitHub ↗</a>`
    : "";
  el.narrativeBody.innerHTML = `<div class="skeleton">${"<div></div>".repeat(6)}</div>`;

  if (!entry) {
    el.narrativeBody.innerHTML = `<div class="state"><h2>Not narrated yet</h2>
      <p><code>${escapeHtml(path)}</code> has no entry on <code>${escapeHtml(state.explainerBranch)}</code>.</p>
      <p>That is Nexus working normally — a file gets an entry the first time the
      <code>narrate</code> skill runs over a commit that touched it.</p></div>`;
    return;
  }

  let content;
  try {
    content = await fetchText(rawUrl(state.explainerBranch, entry.explainerPath));
  } catch (err) {
    el.narrativeBody.innerHTML = `<div class="state"><h2>Couldn't read the entry</h2><p>${escapeHtml(String(err))}</p></div>`;
    return;
  }
  if (state.file !== path) return; // the visitor moved on while this loaded

  const { fm, body, hasFrontmatter } = parseFrontmatter(content);
  const desynced = isDesynced(content, fm, hasFrontmatter);

  const container = document.createElement("div");

  if (hasFrontmatter && (fm.summary || fm.source_commit || desynced)) {
    const card = document.createElement("div");
    card.className = "meta-card";
    const commit = typeof fm.source_commit === "string" ? fm.source_commit : "";
    card.innerHTML = `
      ${fm.summary ? `<p class="summary">${escapeHtml(String(fm.summary))}</p>` : ""}
      <div class="facts">
        ${desynced ? '<span class="chip desync">⚠️ desync flagged</span>' : '<span class="chip">✓ in sync</span>'}
        ${commit ? `<a class="chip" target="_blank" rel="noopener"
            href="https://github.com/${state.owner}/${state.repo}/commit/${encodeURIComponent(commit)}">narrated at ${escapeHtml(commit.slice(0, 7))} ↗</a>` : ""}
      </div>`;
    container.append(card);
  }

  container.append(renderMarkdown(body));

  if (Array.isArray(fm.tests) && fm.tests.length) {
    const tests = document.createElement("div");
    tests.className = "tests";
    tests.innerHTML = `<h2>What the tests verify</h2><dl>${fm.tests
      .map((t) => `<dt>${escapeHtml(String(t?.name ?? ""))}</dt><dd>${escapeHtml(String(t?.intent ?? ""))}</dd>`)
      .join("")}</dl>`;
    container.append(tests);
  }

  el.narrativeBody.replaceChildren(container);
}

async function renderCode(path, line) {
  el.codeTitle.textContent = path;
  el.codeLinks.innerHTML = `
    <a href="https://github.com/${state.owner}/${state.repo}/blob/${encodeURIComponent(state.codeRef)}/${path}${line ? `#L${line}` : ""}" target="_blank" rel="noopener">GitHub ↗</a>
    <a href="https://github.dev/${state.owner}/${state.repo}/blob/${encodeURIComponent(state.codeRef)}/${path}" target="_blank" rel="noopener">github.dev ↗</a>`;

  if (state.view === "narrative") return; // don't fetch what nobody is looking at
  el.codeBody.innerHTML = `<div class="skeleton">${"<div></div>".repeat(10)}</div>`;

  let source;
  try {
    source = await fetchText(rawUrl(state.codeRef, path));
  } catch (err) {
    const missing = err instanceof HttpError && err.status === 404;
    el.codeBody.innerHTML = `<div class="state"><h2>${missing ? "No such file on the code branch" : "Couldn't load the code"}</h2>
      <p>${missing
        ? `<code>${escapeHtml(path)}</code> has an explainer entry but isn't on <code>${escapeHtml(state.codeRef)}</code> any more — it was probably renamed or deleted after it was narrated.`
        : escapeHtml(String(err))}</p></div>`;
    return;
  }
  if (state.file !== path) return;

  el.codeBody.replaceChildren(buildCodeView(source, path, line));
  if (line) {
    const target = el.codeBody.querySelector(`#L${line}`);
    if (target) target.scrollIntoView({ block: "center", behavior: "smooth" });
  }
}

/** Highlights the whole file (so multi-line strings and comments keep their
 *  context) and then splits the result into per-line rows, reopening any
 *  span that straddles a newline — that per-line structure is what lets a
 *  tour stop anchor and highlight a single line. */
function buildCodeView(source, path, hitLine) {
  const extension = path.slice(path.lastIndexOf(".") + 1).toLowerCase();
  const language = window.hljs.getLanguage(extension) ? extension : null;
  const highlighted = language
    ? window.hljs.highlight(source, { language, ignoreIllegals: true }).value
    : window.hljs.highlightAuto(source).value;

  const lines = splitHighlightedLines(highlighted);
  const view = document.createElement("div");
  view.className = "codeview hljs";
  const gutter = document.createElement("div");
  gutter.className = "gutter";
  const body = document.createElement("div");
  body.className = "lines";
  gutter.innerHTML = lines
    .map((_, i) => `<div class="${i + 1 === hitLine ? "hit" : ""}">${i + 1}</div>`)
    .join("");
  body.innerHTML = lines
    .map((html, i) => `<div class="line${i + 1 === hitLine ? " hit" : ""}" id="L${i + 1}">${html || "​"}</div>`)
    .join("");
  view.append(gutter, body);
  return view;
}

function splitHighlightedLines(html) {
  const template = document.createElement("template");
  template.innerHTML = html;

  const lines = [];
  const open = [];
  let current = "";
  const openTag = (node) => `<span class="${escapeHtml(node.getAttribute("class") || "")}">`;

  const walk = (parent) => {
    for (const node of parent.childNodes) {
      if (node.nodeType === Node.TEXT_NODE) {
        const segments = node.data.split("\n");
        segments.forEach((segment, index) => {
          if (index > 0) {
            current += "</span>".repeat(open.length);
            lines.push(current);
            current = open.map(openTag).join("");
          }
          current += escapeHtml(segment);
        });
      } else if (node.nodeType === Node.ELEMENT_NODE) {
        open.push(node);
        current += openTag(node);
        walk(node);
        current += "</span>";
        open.pop();
      }
    }
  };

  walk(template.content);
  lines.push(current);
  return lines;
}

/* ------------------------------------------------------------------ *
 * Tours
 * ------------------------------------------------------------------ */

async function startTour(slug, stopIndex = 0) {
  const tour = state.tours.find((t) => t.slug === slug);
  if (!tour) {
    toast(`No tour named "${slug}"`);
    return;
  }
  if (!tour.stops) {
    const { fm } = parseFrontmatter(await fetchText(rawUrl(state.explainerBranch, tour.explainerPath)));
    tour.title = typeof fm.title === "string" ? fm.title : slug;
    tour.stops = Array.isArray(fm.stops) ? fm.stops : [];
  }
  if (!tour.stops.length) {
    toast("That tour has no stops");
    return;
  }
  state.tour = { slug, title: tour.title, stops: tour.stops, index: Math.min(Math.max(stopIndex, 0), tour.stops.length - 1) };
  if (state.view === "narrative") setView("split");
  renderTour();
}

function renderTour() {
  const tour = state.tour;
  if (!tour) {
    el.tourbar.hidden = true;
    el.tourbar.innerHTML = "";
    return;
  }
  const stop = tour.stops[tour.index];
  el.tourbar.hidden = false;
  el.tourbar.innerHTML = `
    <span class="tour-title">🧭 ${escapeHtml(tour.title || tour.slug)}</span>
    <span class="tour-count">stop ${tour.index + 1} / ${tour.stops.length}</span>
    <span class="tour-note">${escapeHtml(String(stop?.note ?? ""))}</span>
    <span class="tour-nav">
      <button type="button" data-tour="prev" ${tour.index === 0 ? "disabled" : ""}>← Prev</button>
      <button type="button" data-tour="next" ${tour.index === tour.stops.length - 1 ? "disabled" : ""}>Next →</button>
      <button type="button" data-tour="exit">Exit tour</button>
    </span>`;
  renderTree();
  selectFile(String(stop?.path ?? ""), { line: Number(stop?.line) || null, keepTour: true });
}

el.tourbar.addEventListener("click", (event) => {
  const action = event.target.closest("button")?.dataset.tour;
  if (!action || !state.tour) return;
  if (action === "exit") {
    state.tour = null;
    renderTour();
    renderTree();
    syncUrl();
    return;
  }
  state.tour.index += action === "next" ? 1 : -1;
  renderTour();
});

/* ------------------------------------------------------------------ *
 * Navigation / routing
 * ------------------------------------------------------------------ */

function selectFile(path, { line = null, keepTour = false } = {}) {
  if (!path) return;
  if (!keepTour && state.tour) {
    state.tour = null;
    renderTour();
  }
  state.file = path;
  state.line = line;
  el.tree.querySelectorAll(".entry[data-path]").forEach((node) => {
    const current = node.dataset.path === path;
    node.setAttribute("aria-current", current ? "true" : "false");
    // Deep links and tour stops select files the visitor never scrolled to.
    if (current) node.scrollIntoView({ block: "nearest" });
  });
  el.layout.classList.remove("sidebar-open");
  el.sidebarToggle.setAttribute("aria-expanded", "false");
  syncUrl();
  renderNarrative(path);
  renderCode(path, line);
}

function paramsFor(overrides = {}) {
  const params = {
    repo: `${state.owner}/${state.repo}`,
    branch: state.explainerBranch === DEFAULT_EXPLAINER_BRANCH ? null : state.explainerBranch,
    code: state.codeRef === DEFAULT_CODE_REF ? null : state.codeRef,
    file: state.file,
    line: state.line,
    tour: state.tour?.slug ?? null,
    stop: state.tour?.index ? state.tour.index + 1 : null,
    view: state.view === "narrative" ? null : state.view,
    ...overrides,
  };
  // Hand-rolled rather than URLSearchParams so slashes in repo and file
  // paths stay readable — a shared link is the whole point of this page.
  return Object.entries(params)
    .filter(([, value]) => value != null && value !== "")
    .map(([key, value]) => `${key}=${encodeURIComponent(String(value)).replace(/%2F/g, "/")}`)
    .join("&");
}

function syncUrl() {
  history.replaceState(null, "", `${location.pathname}?${paramsFor()}`);
}

function setView(view) {
  state.view = view;
  el.panes.dataset.view = view;
  document.querySelectorAll(".segmented button").forEach((button) => {
    button.setAttribute("aria-pressed", button.dataset.view === view ? "true" : "false");
  });
  syncUrl();
  if (view !== "narrative" && state.file) renderCode(state.file, state.line);
}

/** Applies the ?file / ?tour part of the URL once the branch listing is in.
 *  Reads the route captured at boot, not location.search: syncUrl() rewrites
 *  the address bar from `state` as soon as anything renders, which would
 *  otherwise erase the very deep link this is meant to honor. */
function applyRoute() {
  const route = initialRoute;
  initialRoute = null;
  if (route?.tour) return startTour(route.tour, Math.max(0, Number(route.stop || 1) - 1));
  if (route?.file) return selectFile(route.file, { line: Number(route.line) || null });
  return showWelcome();
}

/* ------------------------------------------------------------------ *
 * Wiring
 * ------------------------------------------------------------------ */

el.repoForm.addEventListener("submit", (event) => {
  event.preventDefault();
  const parsed = parseRepoInput(el.repoInput.value);
  if (!parsed) {
    toast("Enter a repository as owner/repo, or paste its GitHub URL");
    return;
  }
  state.owner = parsed.owner;
  state.repo = parsed.repo;
  state.file = null;
  state.line = null;
  state.tour = null;
  state.filter = "";
  el.filter.value = "";
  renderTour();
  syncUrl();
  loadRepo();
});

el.filter.addEventListener("input", () => {
  state.filter = el.filter.value.trim();
  renderTree();
});

document.querySelector(".segmented").addEventListener("click", (event) => {
  const view = event.target.closest("button")?.dataset.view;
  if (view) setView(view);
});

el.copyLink.addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(location.href);
    toast("Link copied");
  } catch {
    toast(location.href);
  }
});

el.sidebarToggle.addEventListener("click", () => {
  const open = el.layout.classList.toggle("sidebar-open");
  el.sidebarToggle.setAttribute("aria-expanded", String(open));
});

document.addEventListener("keydown", (event) => {
  if (event.target.matches("input, textarea")) return;
  if (event.key === "/") {
    event.preventDefault();
    el.filter.focus();
  } else if (state.tour && (event.key === "ArrowRight" || event.key === "ArrowLeft")) {
    const next = state.tour.index + (event.key === "ArrowRight" ? 1 : -1);
    if (next >= 0 && next < state.tour.stops.length) {
      state.tour.index = next;
      renderTour();
    }
  }
});

/* theme */
const storedTheme = localStorage.getItem("nexus-viewer-theme");
const applyTheme = (theme) => {
  if (theme) document.documentElement.dataset.theme = theme;
  else delete document.documentElement.dataset.theme;
  const dark = theme ? theme === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
  document.querySelectorAll("link[data-theme-style]").forEach((link) => {
    link.disabled = link.dataset.themeStyle !== (dark ? "dark" : "light");
  });
};
applyTheme(storedTheme);
matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
  if (!localStorage.getItem("nexus-viewer-theme")) applyTheme(null);
});
el.themeToggle.addEventListener("click", () => {
  const dark = document.documentElement.dataset.theme
    ? document.documentElement.dataset.theme === "dark"
    : matchMedia("(prefers-color-scheme: dark)").matches;
  const next = dark ? "light" : "dark";
  localStorage.setItem("nexus-viewer-theme", next);
  applyTheme(next);
});

/* boot */
let initialRoute = null;
{
  const params = new URLSearchParams(location.search);
  const parsed = parseRepoInput(params.get("repo") || DEFAULT_REPO) || parseRepoInput(DEFAULT_REPO);
  state.owner = parsed.owner;
  state.repo = parsed.repo;
  state.explainerBranch = params.get("branch") || DEFAULT_EXPLAINER_BRANCH;
  state.codeRef = params.get("code") || DEFAULT_CODE_REF;
  initialRoute = {
    file: params.get("file"),
    line: params.get("line"),
    tour: params.get("tour"),
    stop: params.get("stop"),
  };
  el.repoInput.value = `${state.owner}/${state.repo}`;
  const view = params.get("view");
  setView(["narrative", "split", "code"].includes(view) ? view : "narrative");
  loadRepo();
}
