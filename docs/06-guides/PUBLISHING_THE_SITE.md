# Publishing the Documentation Site

The site is generated from this repository: every Markdown page, the plan's checkboxes (for the Progress page and the home page) and the source files (for the code browser). It is plain HTML, CSS and JavaScript with fonts, Mermaid and search bundled locally, so it works from any static host or straight from disk.

## Build it

```sh
make site          # needs Node.js 18+; output in build/site/
```

`make site` installs the generator's packages (`marked`, `highlight.js`, `mermaid`) on first use, builds the site, then runs `tools/site/check-links.mjs`, which fails if any internal link or `#anchor` is broken. Open `build/site/index.html` in a browser to look at it.

## Configure it

Edit `tools/site/site.config.json`:

| Key | Effect |
|---|---|
| `title`, `tagline`, `description` | Header, home page and search-engine description |
| `repoUrl` | e.g. `https://github.com/you/burrow`. Turns on "Edit this page" links (leave empty while the repository is private) |
| `branch` | Branch used in those links (default `main`) |

`SITE_DATE=YYYY-MM-DD make site` fixes the "Built from the repository on" date, for reproducible output.

## Keep it current

Nothing on the site is typed in by hand except the home page's layer descriptions (`STRATA` in `tools/site/lib/home.mjs`). After a session:

- **Progress, home page and wave banners** count `[x]`, `[~]` and `[ ]` on the `### [ ] Wn-Snn` slice headings of every wave listed in `docs/04-plan/waves/README.md`, and the checkboxes in `BACKLOG.md`. An epic without checkboxes counts as done when every wave its `*Exit:*` line names is done. The first wave that is not done is the current one.
- **Milestones** come from the table in `ROADMAP.md`; a milestone is done when its wave is.
- **Devlog entries** appear as a timeline when they use the `## YYYY-MM-DD — title` heading; **bug entries** become cards when they use `### BUG-NNNN:` under `## Open` or `## Closed`.
- **Test counts** are read from the test tree: `RUN(` lines in `tests/unit/c/test_main.c`, `func Test` in Go, ordered markers in `tests/smoke/expect/*.txt`, `[ktest]` lines in `tests/ktest/expect/*.txt`, `case_run REG-` in `tests/regression/run_all.sh`.
- **A new wave** needs a row in the waves table and, if it starts a new layer, an entry in `STRATA`.

Then run `make site` and publish `build/site`.

## Deploy it

| Host | How |
|---|---|
| GitHub Pages | Push `build/site` to a `gh-pages` branch, or use an Actions workflow that runs `make site` and uploads `build/site` with `actions/upload-pages-artifact`. A `.nojekyll` file is already included. |
| Netlify | Build command `make site`, publish directory `build/site`. Or drag the folder onto the Netlify dashboard. |
| Cloudflare Pages | Same as Netlify. |
| Any web server | Copy `build/site` to the web root. `404.html` is included. |
| A local copy | Replace the folder's contents with `build/site` (delete the old files first: pages for renamed documents would otherwise stay). |

All links are relative, so the site also works under a sub-path such as `https://you.github.io/burrow/`.

## How it is built (for changing the site itself)

| File | Job |
|---|---|
| `tools/site/build.mjs` | Navigation order (`NAV`), page list, link resolution, search index, writing the output |
| `tools/site/lib/md.mjs` | Markdown rendering: heading ids (GitHub-compatible), code highlighting, Mermaid figures, checkbox markers |
| `tools/site/lib/plan.mjs` | Reading the plan and the test tree into numbers (`progress.json` in the output) |
| `tools/site/lib/home.mjs` | The home page's strata and the Progress page |
| `tools/site/lib/source.mjs` | The code browser: a page per tracked text file, an index per top-level directory |
| `tools/site/lib/layout.mjs` | The page shell: header, sidebar, table of contents, pager, dialogs |
| `tools/site/assets/site.css` | Both colour schemes, layout, diagrams, tables |
| `tools/site/assets/site.js` | Theme toggle, search, table of contents, copy buttons, diagram rendering and the zoomable viewer |
| `tools/site/check-links.mjs` | Link and anchor checker |

New Markdown files appear automatically (under "More" in the sidebar until you add them to `NAV` in `build.mjs`). Links between Markdown files, to source files and to directories are rewritten to the matching pages; a link to something the site does not have becomes plain text. `CLAUDE.md` is left out.
