// gallery.mjs - the Gallery page: every screenshot the smoke tests take,
// grouped by wave, oldest first, each with the slice that introduced it.
// Everything comes from docs/gallery/gallery.json, which
// scripts/screens_to_gallery.py writes from the tests and git history; this
// file only reads it and never invents a date, slice or caption.
// Docs: docs/06-guides/PUBLISHING_THE_SITE.md.
import fs from 'node:fs';
import path from 'node:path';
import { esc } from './md.mjs';

export const GALLERY_JSON = 'docs/gallery/gallery.json';

// loadGallery returns the manifest's screenshots whose images exist, or [].
export function loadGallery(root) {
  const file = path.join(root, GALLERY_JSON);
  if (!fs.existsSync(file)) return [];
  const shots = JSON.parse(fs.readFileSync(file, 'utf8')).screenshots || [];
  return shots.filter((s) => fs.existsSync(path.join(root, s.image)) && fs.existsSync(path.join(root, s.thumb)));
}

// copyGallery puts the images into the site at their repository paths.
export function copyGallery(root, out, shots) {
  for (const s of shots) {
    for (const f of [s.image, s.thumb]) {
      fs.mkdirSync(path.dirname(path.join(out, f)), { recursive: true });
      fs.copyFileSync(path.join(root, f), path.join(out, f));
    }
  }
}

const md = (s) => esc(s).replace(/`([^`]+)`/g, '<code>$1</code>');
const waveAnchor = (w) => (w ? w.toLowerCase() : 'other');

// link(repoPath) gives a site-relative URL for a repository path (with an
// optional #anchor), or null; commitUrl(hash) gives a URL or null.
function shot(s, link, commitUrl) {
  const slice = s.slice_link && link(s.slice_link);
  const sliceHtml = slice
    ? `<a href="${esc(slice)}">${esc(s.slice_title || s.slice_label)}</a>`
    : `<span>${esc(s.slice_label || 'Slice not recorded')}</span>`;
  const keys = link(s.keys);
  const screen = s.screen && link(s.screen);
  const commit = s.commit ? (commitUrl(s.commit) ? `<a class="mono" href="${esc(commitUrl(s.commit))}" rel="noopener">${esc(s.commit)}</a>` : `<span class="mono">${esc(s.commit)}</span>`) : '';
  const notes = [s.shows, ...(s.checks || [])].filter(Boolean);
  const checks = notes.length
    ? `<details class="shot-checks"><summary>What the test checks</summary><ul>${notes.map((n) => `<li>${md(n)}</li>`).join('')}</ul>${screen ? `<p>Pixel checks: <a href="${esc(screen)}">${esc(s.screen)}</a></p>` : ''}</details>`
    : '';
  const alt = `Screenshot: ${s.caption}`;
  return `<li class="shot" id="${esc(s.name)}">
  <a class="shot-img" href="${esc(link(s.image))}" data-image data-title="${esc(s.slice_label ? `${s.slice_label}: ${s.name}` : s.name)}" data-w="${s.width}" data-h="${s.height}"><img src="${esc(link(s.thumb))}" alt="${esc(alt)}" width="${s.width / 2}" height="${s.height / 2}" loading="lazy"></a>
  <div class="shot-text">
    <p class="shot-when">${s.date ? `<time datetime="${esc(s.date)}">${esc(s.date)}</time>` : '<span>Date not recorded</span>'}${commit ? ` · ${commit}` : ''}</p>
    <h3 class="shot-slice">${sliceHtml}</h3>
    <p class="shot-caption">${md(s.caption)}</p>
    <p class="shot-meta">Test ${keys ? `<a class="mono" href="${esc(keys)}">${esc(s.test)}</a>` : `<span class="mono">${esc(s.test)}</span>`} · <span class="mono">screen-${esc(s.name)}.ppm</span></p>
    ${checks}
  </div>
</li>`;
}

// galleryBody renders the page; heads are its table of contents.
export function galleryBody(shots, link, commitUrl) {
  const waves = [];
  for (const s of shots) {
    let w = waves[waves.length - 1];
    if (!w || w.id !== s.wave) waves.push((w = { id: s.wave, title: s.wave_title, file: s.wave_file, shots: [] }));
    w.shots.push(s);
  }
  const heads = waves.map((w) => ({ depth: 2, id: waveAnchor(w.id), text: w.id ? `${w.id} ${w.title || ''}`.trim() : 'Other' }));
  const sections = waves.map((w) => {
    const page = w.file && link(w.file);
    const title = w.id ? `${esc(w.id)} ${esc(w.title || '')}` : 'Other';
    return `<h2 id="${waveAnchor(w.id)}">${title}</h2>
<p class="shot-wave">${w.shots.length} screenshot${w.shots.length > 1 ? 's' : ''}${page ? ` · <a href="${esc(page)}">The wave's plan</a>` : ''}</p>
<ol class="shots">
${w.shots.map((s) => shot(s, link, commitUrl)).join('\n')}
</ol>`;
  }).join('\n');
  const first = shots.find((s) => s.date)?.date;
  const last = [...shots].reverse().find((s) => s.date)?.date;
  const body = `<p class="crumbs">Project status</p><h1>Gallery</h1>
<p class="lede">Burrow's screen as the automated tests see it. Each smoke test boots Burrow in QEMU, types into it, saves the screen with <code>screendump</code> and checks pixels in it; here are those screens, grouped by wave, oldest first${first ? ` (${esc(first)} to ${esc(last)})` : ''}, each with the slice that added it. Select a picture to see it full size.</p>
${shots.length ? sections : '<p>No screenshots yet: run the smoke tests, then <code>python3 scripts/screens_to_gallery.py</code>.</p>'}`;
  return { body, heads, text: shots.map((s) => `${s.slice_label || ''} ${s.test} ${s.caption}`).join(' ') };
}
