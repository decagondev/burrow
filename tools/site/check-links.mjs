// check-links.mjs - fail if any page of the built site links to a page,
// asset or #anchor that does not exist. Only relative links are checked;
// external ones are counted and left alone. It never changes the site.
// Usage: node check-links.mjs <site-dir>. Docs: docs/06-guides/PUBLISHING_THE_SITE.md.
import fs from 'node:fs';
import path from 'node:path';

const SITE = path.resolve(process.argv[2] || '../../build/site');
const files = [];
const walk = (d) => fs.readdirSync(d, { withFileTypes: true }).forEach((e) => {
  const p = path.join(d, e.name);
  if (e.isDirectory()) walk(p); else if (p.endsWith('.html')) files.push(p);
});
walk(SITE);

const ids = new Map();
const idsOf = (file) => {
  if (!ids.has(file)) {
    const html = fs.readFileSync(file, 'utf8');
    ids.set(file, new Set([...html.matchAll(/\sid="([^"]+)"/g)].map((m) => m[1].replace(/&amp;/g, '&').replace(/&quot;/g, '"'))));
  }
  return ids.get(file);
};

let links = 0;
let external = 0;
const problems = [];
for (const file of files) {
  const html = fs.readFileSync(file, 'utf8');
  for (const m of html.matchAll(/\s(?:href|src)="([^"]*)"/g)) {
    const raw = m[1].replace(/&amp;/g, '&');
    if (/^(https?:|mailto:|data:)/.test(raw)) { external++; continue; }
    links++;
    const [p, hash] = raw.split('#');
    const target = p ? path.resolve(path.dirname(file), decodeURIComponent(p)) : file;
    if (!fs.existsSync(target)) { problems.push(`${path.relative(SITE, file)}: missing ${raw}`); continue; }
    if (hash && target.endsWith('.html') && !idsOf(target).has(decodeURIComponent(hash))) problems.push(`${path.relative(SITE, file)}: no #${hash} in ${path.relative(SITE, target)}`);
  }
}
problems.slice(0, 50).forEach((p) => console.log(`LINK ${p}`));
console.log(`links: ${files.length} pages, ${links} internal links, ${external} external, ${problems.length} problems`);
process.exit(problems.length ? 1 : 0);
