// source.mjs - the code browser: one highlighted page with line numbers per
// tracked text file, an index page per top-level directory, and an overview.
// Markdown files are not here (they are pages); binary files are skipped.
// Vendored Go modules (kernel/vendor, W17) are not ours to show. It never
// reads files git does not track.
// Docs: docs/06-guides/PUBLISHING_THE_SITE.md.
import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { esc, highlight, slug } from './md.mjs';

const BY_EXT = {
  '.c': ['C', 'c'], '.h': ['C', 'c'], '.go': ['Go', 'go'], '.s': ['Go assembly', 'x86asm'], '.asm': ['NASM', 'x86asm'],
  '.inc': ['NASM', 'x86asm'], '.ld': ['Linker script', 'plaintext'], '.sh': ['Shell', 'bash'], '.py': ['Python', 'python'],
  '.mjs': ['JavaScript', 'javascript'], '.js': ['JavaScript', 'javascript'], '.json': ['JSON', 'json'], '.css': ['CSS', 'css'],
  '.yml': ['YAML', 'yaml'], '.yaml': ['YAML', 'yaml'], '.cpp': ['C++', 'cpp'], '.lua': ['Lua', 'lua'], '.mod': ['Go module', 'plaintext'],
  '.txt': ['Text', 'plaintext'], '.keys': ['Key script', 'plaintext'], '.screen': ['Screen checks', 'plaintext'], '.config': ['Config', 'ini'],
  '.svg': ['SVG', 'xml'],
};

export function language(file) {
  const base = path.basename(file);
  if (base === 'Makefile') return ['Makefile', 'makefile'];
  if (base === 'gdbinit') return ['GDB script', 'plaintext'];
  return BY_EXT[path.extname(file)] || ['Text', 'plaintext'];
}

// files lists the tracked text files the browser shows.
export function sourceFiles(root) {
  const all = execFileSync('git', ['ls-files', '-z'], { cwd: root, encoding: 'utf8', maxBuffer: 64 << 20 }).split('\0').filter(Boolean);
  return all.filter((f) => {
    if (f.endsWith('.md') || /(^|\/)(fonts|node_modules|vendor)\//.test(f) || f.endsWith('.woff2')) return false;
    const p = path.join(root, f);
    if (!fs.existsSync(p) || fs.statSync(p).size > 400_000) return false;
    return !fs.readFileSync(p).includes(0);
  }).sort();
}

export const sourceUrl = (f) => `source/${f}.html`;
export const topOf = (f) => (f.includes('/') ? f.split('/')[0] : '');
export const dirUrl = (top) => (top ? `source/${top}/index.html` : 'source/index.html');

export function sourcePage(root, f) {
  const text = fs.readFileSync(path.join(root, f), 'utf8').replace(/\r\n/g, '\n').replace(/\n$/, '');
  const [name, lang] = language(f);
  const n = text.split('\n').length;
  const gutter = Array.from({ length: n }, (_, i) => `<span id="L${i + 1}">${i + 1}</span>`).join('\n');
  const { cls, html } = highlight(text, lang);
  const body = `<p class="crumbs">Source code</p><h1 class="mono path">${esc(f)}</h1>
<p class="source-meta">${esc(name)}, ${n} line${n === 1 ? '' : 's'}. In <a href="${'../'.repeat(f.split('/').length)}${dirUrl(topOf(f))}">${esc(topOf(f) ? `${topOf(f)}/` : 'the top level')}</a>.</p>
<div class="code-block source wide"><div class="code-head"><span>${esc(path.basename(f))}</span></div><div class="source-grid"><pre class="gutter" aria-hidden="true">${gutter}</pre><pre><code class="hljs language-${cls}">${html}</code></pre></div></div>`;
  return { body, lines: n, text };
}

const lineCount = (root, f) => fs.readFileSync(path.join(root, f), 'utf8').replace(/\n$/, '').split('\n').length;

function fileTable(root, from, files, strip) {
  const rows = files.map((f) => `<tr><td><a class="mono" href="${'../'.repeat(from.split('/').length - 1)}${sourceUrl(f)}">${esc(f.slice(strip))}</a></td><td>${esc(language(f)[0])}</td><td>${lineCount(root, f)}</td></tr>`).join('');
  return `<div class="table-wrap wide"><table><thead><tr><th>File</th><th>Language</th><th>Lines</th></tr></thead><tbody>${rows}</tbody></table></div>`;
}

// dirPage lists one top-level directory, grouped by its subdirectories.
export function dirPage(root, top, files) {
  const url = dirUrl(top);
  const groups = new Map();
  for (const f of files) {
    const d = path.posix.dirname(f.slice(top.length + 1));
    if (!groups.has(d)) groups.set(d, []);
    groups.get(d).push(f);
  }
  const heads = [];
  let body = `<p class="crumbs">Source code</p><h1 class="mono path">${esc(top)}/</h1>\n<p class="source-meta">${files.length} files.</p>\n`;
  for (const [d, fs_] of groups) {
    if (d !== '.') {
      const id = slug(d.replace(/\//g, ' '));
      heads.push({ depth: 2, id, text: `${d}/` });
      body += `<h2 id="${id}" class="mono">${esc(d)}/</h2>\n`;
    }
    body += fileTable(root, url, fs_, top.length + 1 + (d === '.' ? 0 : d.length + 1));
  }
  return { url, title: `${top}/`, body, heads };
}

export function overviewPage(root, tops, topFiles, counts) {
  const rows = tops.map((t) => `<tr><td><a class="mono" href="${t}/index.html">${esc(t)}/</a></td><td>${counts.get(t)}</td></tr>`).join('');
  const body = `<p class="crumbs">Source code</p><h1 id="source-code">Source code</h1>
<p>Every file in the repository except the Markdown documents (those are the pages of this site), with line numbers you can link to (<code>#L42</code>).</p>
<div class="table-wrap wide"><table><thead><tr><th>Directory</th><th>Files</th></tr></thead><tbody>${rows}</tbody></table></div>
<h2 id="top-level">Top level</h2>
${fileTable(root, 'source/index.html', topFiles, 0)}`;
  return { url: 'source/index.html', title: 'Source code', body, heads: [{ depth: 2, id: 'top-level', text: 'Top level' }] };
}
