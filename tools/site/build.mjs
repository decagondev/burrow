// build.mjs - generate the static documentation site (make site): every
// Markdown file becomes a page, the plan becomes the Progress page and the
// home page's strata, and every tracked text file gets a code-browser page.
// Output goes to build/site (or the directory given as the first argument);
// the repository is only read, never written.
// Docs: docs/06-guides/PUBLISHING_THE_SITE.md.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
import { render, wrapEntries, slug, stripTags, esc } from './lib/md.mjs';
import { page, rel } from './lib/layout.mjs';
import { progress } from './lib/plan.mjs';
import { homeBody, progressBody, pill, bar } from './lib/home.mjs';
import { sourceFiles, sourcePage, sourceUrl, dirPage, overviewPage, topOf, dirUrl } from './lib/source.mjs';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(HERE, '../..');
const OUT = path.resolve(process.argv[2] || path.join(ROOT, 'build/site'));
const site = { ...JSON.parse(fs.readFileSync(path.join(HERE, 'site.config.json'), 'utf8')), date: process.env.SITE_DATE || new Date().toISOString().slice(0, 10) };
site.branch ||= 'main';

// Navigation: groups in reading order; Markdown files not listed go to "More".
const NAV = [
  ['Start here', ['README.md', 'docs/README.md', 'docs/00-vision/VISION.md']],
  ['Architecture', ['docs/02-architecture/ARCHITECTURE.md', 'docs/02-architecture/MEMORY_MAP.md', 'docs/02-architecture/BOOT_PROTOCOL.md', 'docs/02-architecture/GO_RUNTIME_HOSTING.md', 'docs/02-architecture/NUCLEUS_ABI.md', 'docs/02-architecture/POSIX_COVERAGE.md']],
  ['Plan', ['docs/04-plan/PLAN_OVERVIEW.md', 'docs/04-plan/ROADMAP.md', 'docs/04-plan/BACKLOG.md', 'docs/04-plan/waves/README.md', 'docs/04-plan/waves/WAVE-*', 'docs/04-plan/waves/WAVES-07-12.md']],
  ['Living docs', ['docs/living/DEVLOG.md', 'docs/living/BUGS_AND_MITIGATIONS.md', 'docs/living/RISKS.md']],
  ['Decisions', ['docs/03-decisions/README.md', 'docs/03-decisions/ADR-0*']],
  ['Research', ['docs/01-research/RESEARCH.md', 'docs/01-research/GO_RUNTIME_SYSCALL_CENSUS.md']],
  ['Testing', ['docs/05-testing/TEST_STRATEGY.md']],
  ['Guides', ['docs/06-guides/TOOLCHAIN_SETUP.md', 'docs/06-guides/WORKFLOW.md', 'docs/06-guides/WORKBENCH.md', 'docs/06-guides/PORTING.md', 'docs/06-guides/DEBUGGING.md', 'docs/06-guides/CODING_STANDARDS.md', 'docs/06-guides/PUBLISHING_THE_SITE.md', 'docs/06-guides/GLOSSARY.md']],
  ['Reference code', ['snippets/nucleus/README.md']],
];
const SKIP_MD = new Set(['CLAUDE.md']);
const NAV_TITLE = { 'README.md': 'Project overview', 'docs/README.md': 'How the docs are organised', 'docs/04-plan/waves/README.md': 'All waves', 'docs/03-decisions/README.md': 'Decision index', 'snippets/nucleus/README.md': 'Nucleus snippets', 'docs/03-decisions/ADR-0000-template.md': 'ADR template' };

const tracked = execFileSync('git', ['ls-files', '-z', '--cached', '--others', '--exclude-standard'], { cwd: ROOT, encoding: 'utf8', maxBuffer: 64 << 20 })
  .split('\0').filter((f) => f && !/^\.(?!github)/.test(f) && fs.existsSync(path.join(ROOT, f)));
const mdFiles = tracked.filter((f) => f.endsWith('.md') && !SKIP_MD.has(f)).sort();
const pageUrl = (f) => (f === 'README.md' ? 'overview.html' : f.endsWith('README.md') ? f.replace(/README\.md$/, 'index.html') : f.replace(/\.md$/, '.html'));
const srcFiles = sourceFiles(ROOT).filter((f) => tracked.includes(f));
const srcSet = new Set(srcFiles);
const dirs = new Set(tracked.flatMap((f) => f.split('/').slice(0, -1).map((_, i, a) => a.slice(0, i + 1).join('/'))));

// Pages from Markdown, with their titles and groups.
const pages = new Map();
for (const f of mdFiles) {
  const text = fs.readFileSync(path.join(ROOT, f), 'utf8');
  const h1 = (text.match(/^# (.+)$/m) || [, path.basename(f, '.md')])[1].trim();
  pages.set(f, { src: f, url: pageUrl(f), title: stripTags(h1.replace(/`/g, '')), text });
}
const glob = (pat) => (pat.endsWith('*') ? mdFiles.filter((f) => f.startsWith(pat.slice(0, -1)) && pat !== f) : [pat]);
const listed = new Set();
const groups = NAV.map(([name, pats]) => {
  const items = pats.flatMap(glob).filter((f) => pages.has(f) && !listed.has(f) && f !== 'docs/03-decisions/ADR-0000-template.md');
  items.forEach((f) => { listed.add(f); pages.get(f).group = name; });
  return { name, files: items };
});
groups.find((g) => g.name === 'Reference code').files.push('docs/03-decisions/ADR-0000-template.md');
listed.add('docs/03-decisions/ADR-0000-template.md');
pages.get('docs/03-decisions/ADR-0000-template.md').group = 'Reference code';
const more = mdFiles.filter((f) => !listed.has(f));
more.forEach((f) => { pages.get(f).group = 'More'; });
if (more.length) groups.push({ name: 'More', files: more });

const tops = [...new Set(srcFiles.map(topOf).filter(Boolean))].sort();
const nav = {
  top: [{ title: 'Home', url: 'index.html' }, { title: 'Progress', url: 'progress.html' }],
  groups: [
    ...groups.map((g) => ({ name: g.name, items: g.files.map((f) => ({ title: NAV_TITLE[f] || pages.get(f).title, url: pages.get(f).url })) })),
    { name: 'Source code', items: [{ title: 'All source', url: 'source/index.html' }, ...tops.map((t) => ({ title: `${t}/`, url: dirUrl(t), mono: true }))] },
  ],
};
const order = groups.flatMap((g) => g.files.map((f) => ({ title: NAV_TITLE[f] || pages.get(f).title, url: pages.get(f).url })));

// resolve turns a link in `from` (a repository path) into a link from its page.
function resolver(from, fromUrl) {
  return (href) => {
    const [p, hash] = href.split('#');
    const target = path.posix.normalize(path.posix.join(path.posix.dirname(from), decodeURIComponent(p))).replace(/\/$/, '');
    let url = null;
    if (pages.has(target)) url = pages.get(target).url;
    else if (pages.has(`${target}/README.md`)) url = pages.get(`${target}/README.md`).url;
    else if (srcSet.has(target)) url = sourceUrl(target);
    else if (dirs.has(target) && tops.includes(topOf(`${target}/x`))) {
      const sub = target.split('/').slice(1).join('/');
      return rel(fromUrl, dirUrl(topOf(`${target}/x`))) + (sub ? `#${slug(sub.replace(/\//g, ' '))}` : '');
    }
    if (!url) return null;
    return rel(fromUrl, url) + (hash ? `#${hash}` : '');
  };
}

const pr = progress(ROOT, site.date);
const waveOf = (f) => pr.waves.find((w) => w.src === f);

function waveBanner(w) {
  if (!w || !w.total) return '';
  return `<div class="wave-banner">${pill(w.status)}<div class="wave-banner-main"><strong>${w.done} of ${w.total} slices done</strong>${bar(w.done, w.total, `${w.id} progress`)}</div><a href="${rel(pageUrl(w.src), 'progress.html')}">All progress</a></div>`;
}

const out = [];
const search = [];
const plainText = (h) => stripTags(h.replace(/<pre class="diagram-src">[\s\S]*?<\/pre>/g, ' ')).replace(/\s+/g, ' ');

for (const pg of pages.values()) {
  const { html, heads } = render(pg.text, { resolve: resolver(pg.src, pg.url) });
  let body = html;
  let kind = 'doc';
  let toc = heads.filter((h) => h.depth === 2 || h.depth === 3);
  if (pg.src === 'docs/living/DEVLOG.md') {
    kind = 'devlog';
    body = body.replace(/<hr>\n?/g, '');
    const isEntry = (t) => /^\d{4}-\d\d-\d\d — /.test(t);
    body = wrapEntries(body, 'h2', isEntry, (h) => h.replace(/>(\d{4}-\d\d-\d\d) — ([\s\S]*)<\/h2>$/, '><time datetime="$1">$1</time><span>$2</span></h2>'));
    toc = heads.filter((h) => h.depth === 2 && isEntry(h.text));
  } else if (pg.src === 'docs/living/BUGS_AND_MITIGATIONS.md') {
    kind = 'bugs';
    body = wrapEntries(body, 'h3', (t) => /^BUG-\d+/.test(t));
  }
  body = body.replace(/(<h1 id="[^"]*">[\s\S]*?<\/h1>)/, `$1${waveBanner(waveOf(pg.src))}`);
  body = `<p class="crumbs">${esc(pg.group)}</p>${body}`;
  const i = order.findIndex((o) => o.url === pg.url);
  out.push({ url: pg.url, title: pg.title, kind, body, heads: toc, prev: order[i - 1], next: order[i + 1], edit: pg.src });
  search.push({ t: pg.title, u: pg.url, g: pg.group, h: heads.filter((h) => h.depth > 1).map((h) => [h.text, h.id]), x: plainText(body).slice(0, 40000) });
}

// Computed pages.
out.push({ url: 'index.html', title: site.title, kind: 'home', body: homeBody(pr), heads: [] });
out.push({ url: 'progress.html', title: 'Progress', kind: 'progress', body: progressBody(pr, slug), heads: ['Milestones', 'Waves', 'Epics', 'Quality signals'].map((t, k) => ({ depth: 2, id: ['milestones', 'waves', 'epics', 'quality'][k], text: t })) });
search.unshift({ t: 'Progress', u: 'progress.html', g: 'Project status', h: [['Milestones', 'milestones'], ['Waves', 'waves'], ['Epics', 'epics'], ['Quality signals', 'quality']], x: 'progress milestones waves epics slices bugs tests' });
out.push({ url: '404.html', title: 'Page not found', kind: 'doc', body: '<h1>Page not found</h1><p>This address does not match any page. It may have been renamed when the docs changed.</p><p><a href="index.html">Go to the home page</a> or press <kbd>/</kbd> to search.</p>', heads: [] });

// The code browser.
const counts = new Map(tops.map((t) => [t, srcFiles.filter((f) => topOf(f) === t).length]));
const ov = overviewPage(ROOT, tops, srcFiles.filter((f) => !topOf(f)), counts);
out.push({ ...ov, kind: 'doc' });
for (const t of tops) out.push({ ...dirPage(ROOT, t, srcFiles.filter((f) => topOf(f) === t)), kind: 'doc' });
for (const f of srcFiles) {
  const sp = sourcePage(ROOT, f);
  out.push({ url: sourceUrl(f), title: f, kind: 'source', body: sp.body, heads: [], edit: f });
  search.push({ t: f, u: sourceUrl(f), g: 'Source code', h: [], x: sp.text.slice(0, 600).replace(/\s+/g, ' ') });
}

// Write everything.
fs.rmSync(OUT, { recursive: true, force: true });
const write = (p, s) => { fs.mkdirSync(path.dirname(path.join(OUT, p)), { recursive: true }); fs.writeFileSync(path.join(OUT, p), s); };
for (const p of out) write(p.url, page(site, nav, p));
fs.cpSync(path.join(HERE, 'assets'), path.join(OUT, 'assets'), { recursive: true });
fs.copyFileSync(path.join(HERE, 'node_modules/mermaid/dist/mermaid.min.js'), path.join(OUT, 'assets/mermaid.min.js'));
write('assets/search-index.js', `window.SEARCH_INDEX=${JSON.stringify(search)};\n`);
write('progress.json', `${JSON.stringify(pr, null, 2)}\n`);
write('.nojekyll', '');
console.log(`site: ${out.length} pages (${pages.size} documents, ${srcFiles.length} source files) -> ${path.relative(ROOT, OUT) || OUT}`);
