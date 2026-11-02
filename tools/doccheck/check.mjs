// check.mjs - usage: node check.mjs <file.md>... ; exits 1 if any Mermaid block fails to parse.
import { JSDOM } from 'jsdom';
import fs from 'fs';
const dom = new JSDOM('<!doctype html><html><body></body></html>');
globalThis.window = dom.window; globalThis.document = dom.window.document;
try { globalThis.navigator = dom.window.navigator; } catch {}
globalThis.DOMParser = dom.window.DOMParser;
const { default: mermaid } = await import('mermaid');
mermaid.initialize({ startOnLoad: false });
let bad = 0, total = 0;
for (const f of process.argv.slice(2)) {
  const md = fs.readFileSync(f, 'utf8');
  const re = /```mermaid\n([\s\S]*?)```/g; let m; let i = 0;
  while ((m = re.exec(md))) {
    i++; total++;
    try { await mermaid.parse(m[1]); }
    catch (e) { bad++; console.log(`FAIL ${f} #${i}: ${String(e.message || e).split('\n').slice(0,4).join(' | ')}`); }
  }
}
console.log(`${total} diagrams, ${bad} failed`);
process.exit(bad ? 1 : 0);
