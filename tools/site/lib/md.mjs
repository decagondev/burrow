// md.mjs - Markdown to HTML for the docs site: GitHub-style heading ids,
// highlighted code blocks, Mermaid blocks as diagram figures, task markers
// for plan checkboxes, and links rewritten from repository paths to pages.
// Never fetches anything; links it cannot resolve become plain text.
// Docs: docs/06-guides/PUBLISHING_THE_SITE.md.
import { Marked } from 'marked';
import hljs from 'highlight.js';

export const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#x27;' }[c]));
export const stripTags = (h) => h.replace(/<[^>]*>/g, '').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&#x27;|&#39;/g, "'").replace(/&amp;/g, '&');

// slug matches GitHub's anchors, so links written for GitHub keep working.
export function slug(text) {
  return text.trim().toLowerCase().replace(/[^\p{L}\p{N}\p{M}\s_-]/gu, '').replace(/\s/g, '-');
}

const LABELS = { sh: 'Shell', bash: 'Shell', shell: 'Shell', console: 'Shell', text: 'Text', txt: 'Text', '': 'Text',
  c: 'C', h: 'C', go: 'Go', asm: 'Assembly', nasm: 'Assembly', make: 'Makefile', makefile: 'Makefile', json: 'JSON',
  python: 'Python', py: 'Python', js: 'JavaScript', mjs: 'JavaScript', yaml: 'YAML', yml: 'YAML', cpp: 'C++', lua: 'Lua', diff: 'Diff' };
const LANGS = { sh: 'bash', shell: 'bash', console: 'bash', text: 'plaintext', txt: 'plaintext', '': 'plaintext',
  asm: 'x86asm', nasm: 'x86asm', make: 'makefile', py: 'python', mjs: 'javascript', yml: 'yaml', h: 'c' };

export function highlight(code, lang) {
  const l = LANGS[lang] ?? lang;
  if (l !== 'plaintext' && hljs.getLanguage(l)) return { cls: l, html: hljs.highlight(code, { language: l, ignoreIllegals: true }).value };
  return { cls: 'plaintext', html: esc(code) };
}

export function codeBlock(code, lang, label) {
  const { cls, html } = highlight(code.replace(/\n$/, ''), (lang || '').toLowerCase());
  const head = label ?? LABELS[(lang || '').toLowerCase()] ?? lang;
  return `<div class="code-block"><div class="code-head"><span>${esc(head)}</span></div><pre><code class="hljs language-${cls}">${html}</code></pre></div>\n`;
}

const MARKS = {
  x: '<span class="tm tm-done" role="img" aria-label="Done" title="Done"></span>',
  '~': '<span class="tm tm-wip" role="img" aria-label="In progress" title="In progress"></span>',
  ' ': '<span class="tm tm-open" role="img" aria-label="Not started" title="Not started"></span>',
};
export const marker = (c) => MARKS[c.toLowerCase()] || MARKS[' '];

// render turns one Markdown file into HTML plus its headings.
// ctx.resolve(href) gives a site-relative URL for a repository link, or null.
export function render(src, ctx) {
  const heads = [];
  const used = new Map();
  const unique = (id) => {
    const n = used.get(id) || 0;
    used.set(id, n + 1);
    return n ? `${id}-${n}` : id;
  };
  const md = new Marked({ gfm: true });
  md.use({
    renderer: {
      heading({ tokens, depth }) {
        let inner = this.parser.parseInline(tokens);
        let mark = '';
        const m = inner.match(/^\[([ xX~])\]\s*/);
        if (m) { mark = marker(m[1]) + ' '; inner = inner.slice(m[0].length); }
        const text = stripTags(inner);
        const id = unique(slug(text) || 'section');
        heads.push({ depth, id, text, html: inner });
        return `<h${depth} id="${esc(id)}">${mark}${inner}</h${depth}>\n`;
      },
      code({ text, lang }) {
        const l = (lang || '').trim().split(/\s+/)[0];
        if (l === 'mermaid') return `<figure class="diagram wide" data-diagram><pre class="diagram-src"><code>${esc(text)}\n</code></pre></figure>\n`;
        return codeBlock(text, l);
      },
      link({ href, title, tokens }) {
        const text = this.parser.parseInline(tokens);
        const url = /^(https?:|mailto:)/.test(href) ? href : href.startsWith('#') ? href : ctx.resolve(href);
        if (url == null) return `<span class="unlinked">${text}</span>`;
        const ext = /^https?:/.test(url) ? ' rel="noopener"' : '';
        return `<a href="${esc(url)}"${title ? ` title="${esc(title)}"` : ''}${ext}>${text}</a>`;
      },
      image({ href, text }) {
        const url = /^https?:/.test(href) ? href : ctx.resolve(href, true);
        return url ? `<img src="${esc(url)}" alt="${esc(text)}" loading="lazy">` : esc(text);
      },
      checkbox({ checked }) { return marker(checked ? 'x' : ' '); },
    },
  });
  let html = md.parse(src);
  html = html.replace(/<table>/g, '<div class="table-wrap wide"><table>').replace(/<\/table>/g, '</table></div>');
  html = html.replace(/<li>\[~\]\s*/g, `<li>${marker('~')} `);
  html = html.replace(/<li><p>\[~\]\s*/g, `<li><p>${marker('~')} `);
  return { html, heads };
}

// wrapEntries puts every heading of `tag` whose text passes `test` and what
// follows it (up to the next heading at its level or above) in a <section
// class="entry">, for the devlog timeline and the bug cards.
export function wrapEntries(html, tag, test, decorate = (h) => h) {
  const level = Number(tag[1]);
  const re = /<h([1-6]) id="[^"]*">[\s\S]*?<\/h\1>/g;
  let out = '';
  let last = 0;
  let open = false;
  for (const m of html.matchAll(re)) {
    const d = Number(m[1]);
    if (d > level) continue;
    out += html.slice(last, m.index);
    if (open) { out += '</section>'; open = false; }
    let h = m[0];
    if (d === level && test(stripTags(h))) { out += '<section class="entry">'; h = decorate(h); open = true; }
    out += h;
    last = m.index + m[0].length;
  }
  out += html.slice(last);
  if (open) out = out.replace(/\s*$/, '') + '</section>';
  return out;
}
