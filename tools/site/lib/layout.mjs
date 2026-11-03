// layout.mjs - the page shell every page shares: head, top bar, sidebar
// navigation, table of contents, previous/next pager, footer and the search
// and diagram dialogs. All links are relative, so the site works from disk
// or under any sub-path. It never embeds absolute URLs to this site.
// Docs: docs/06-guides/PUBLISHING_THE_SITE.md.
import { esc } from './md.mjs';

// rel turns a site path into a link from the page at `from`.
export function rel(from, to) {
  const up = from.split('/').length - 1;
  return '../'.repeat(up) + to;
}

const ICON_MENU = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M4 12h16M4 17h16"/></svg>';
const ICON_SEARCH = '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="6.5"/><path d="m16 16 4.5 4.5"/></svg>';
const ICON_THEME = '<svg class="i-sun" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg><svg class="i-moon" viewBox="0 0 24 24" aria-hidden="true"><path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"/></svg><svg class="i-auto" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="8"/><path d="M12 4a8 8 0 0 1 0 16z" class="fill"/></svg>';
const THEME_BOOT = "<script>(function(){try{var p=localStorage.getItem('burrow-theme')||'system';var d=p==='dark'||(p==='system'&&matchMedia('(prefers-color-scheme: dark)').matches);document.documentElement.dataset.theme=d?'dark':'light';document.documentElement.dataset.themePref=p;}catch(e){}})();</script>";

function sidebar(nav, url) {
  const link = (it) => `<li><a href="${rel(url, it.url)}"${it.url === url ? ' aria-current="page"' : ''}${it.mono ? ' class="mono"' : ''}>${esc(it.title)}</a></li>`;
  const top = `<ul class="nav-top">${nav.top.map(link).join('')}</ul>`;
  const groups = nav.groups.map((g) => `<div class="nav-group"><h2>${esc(g.name)}</h2><ul>${g.items.map(link).join('')}</ul></div>`).join('');
  return `<nav class="sidebar-nav" aria-label="Site">${top}${groups}</nav>`;
}

function toc(heads) {
  if (!heads.length) return '';
  const items = heads.map((h) => `<li class="l${h.depth}"><a href="#${esc(h.id)}">${esc(h.text)}</a></li>`).join('');
  return `<aside class="toc" aria-label="On this page"><h2>On this page</h2><ul>${items}</ul></aside>`;
}

function pager(url, prev, next) {
  if (!prev && !next) return '';
  const a = (p, cls, word) => (p ? `<a class="${cls}" href="${rel(url, p.url)}"><small>${word}</small><span>${esc(p.title)}</span></a>` : '');
  return `<nav class="pager" aria-label="Previous and next page">${a(prev, 'prev', 'Previous')}${a(next, 'next', 'Next')}</nav>`;
}

const DIALOGS = `<dialog class="search-dialog" aria-label="Search">
  <form method="dialog" class="search-form" role="search">${ICON_SEARCH}<input type="search" placeholder="Search pages, headings and text" aria-label="Search" autocomplete="off" spellcheck="false"><kbd>Esc</kbd></form>
  <ul class="search-results" role="listbox" aria-label="Results"></ul>
  <p class="search-hint">Type at least two letters. Use the arrow keys and Enter to open a result.</p>
</dialog>
<dialog class="lightbox" aria-label="Diagram viewer">
  <div class="lb-bar"><span class="lb-title">Diagram</span><div class="lb-tools"><button type="button" data-zoom="out" aria-label="Zoom out">−</button><button type="button" data-zoom="fit">Fit</button><button type="button" data-zoom="in" aria-label="Zoom in">+</button><button type="button" data-zoom="close" aria-label="Close viewer">Close</button></div></div>
  <div class="lb-stage"><div class="lb-canvas"></div></div>
  <p class="lb-help">Drag to pan, scroll or use + and − to zoom, 0 to fit, Esc to close.</p>
</dialog>`;

// page assembles one HTML document. p: { url, title, kind, body, heads,
// prev, next, edit } where body is the article's inner HTML.
export function page(site, nav, p) {
  const R = rel(p.url, '');
  const title = p.url === 'index.html' ? site.title : `${p.title} | ${site.title}`;
  const edit = p.edit && site.repoUrl ? `<a class="edit-link" href="${esc(`${site.repoUrl}/blob/${site.branch}/${p.edit}`)}" rel="noopener">Edit this page</a>` : '';
  const hasDiagram = p.body.includes('data-diagram');
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(title)}</title>
<meta name="description" content="${esc(site.description)}">
<meta name="color-scheme" content="light dark">
<link rel="icon" href="${R}assets/favicon.svg" type="image/svg+xml">
${THEME_BOOT}
<link rel="stylesheet" href="${R}assets/site.css">
</head>
<body class="kind-${p.kind || 'doc'}">
<a class="skip" href="#content">Skip to content</a>
<header class="topbar">
  <button class="icon-btn nav-toggle" type="button" aria-label="Open navigation" aria-expanded="false" aria-controls="sidebar">${ICON_MENU}</button>
  <a class="brand" href="${R}index.html"><img src="${R}assets/favicon.svg" alt="" width="26" height="26"><span>${esc(site.title)}</span></a>
  <button class="search-btn" type="button" aria-label="Search the documentation">${ICON_SEARCH}<span>Search docs</span><kbd>/</kbd></button>
  <div class="topbar-end">
    ${edit}
    <button class="icon-btn theme-toggle" type="button" aria-label="Colour theme">${ICON_THEME}</button>
  </div>
</header>
<div class="layout">
  <aside class="sidebar" id="sidebar">${sidebar(nav, p.url)}</aside>
  <div class="scrim" hidden></div>
  <main id="content" class="content" tabindex="-1">
    <article class="prose">${p.body}</article>
    ${pager(p.url, p.prev, p.next)}
    <footer class="page-foot"><span>Built from the repository on ${esc(site.date)}.</span></footer>
  </main>
  ${toc(p.heads || [])}
</div>
${DIALOGS}
<script>window.SITE_ROOT="${R || './'}";</script>
<script src="${R}assets/search-index.js" defer></script>
${hasDiagram ? `<script src="${R}assets/mermaid.min.js" defer></script>\n` : ''}<script src="${R}assets/site.js" defer></script>
</body>
</html>
`;
}
