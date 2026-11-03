// plan.mjs - read the plan and the test tree into numbers for the Progress
// page, the home page and the wave banners: waves and their slice
// checkboxes, milestones, epics, the latest devlog entry, bug and test counts.
// It only reads files; it never changes them.
// Docs: docs/04-plan/PLAN_OVERVIEW.md, docs/06-guides/PUBLISHING_THE_SITE.md.
import fs from 'node:fs';
import path from 'node:path';

const read = (root, p) => { try { return fs.readFileSync(path.join(root, p), 'utf8'); } catch { return ''; } };
const cells = (line) => line.trim().replace(/^\||\|$/g, '').split('|').map((c) => c.trim());
const tableRows = (text) => text.split('\n').filter((l) => /^\|/.test(l) && !/^\|\s*-/.test(l)).slice(1).map(cells);
const plain = (s) => s.replace(/\*\*/g, '').replace(/`/g, '').trim();

// sliceCounts counts the ### [x] / [~] / [ ] slice headings of a wave file.
export function sliceCounts(text) {
  const c = { total: 0, done: 0, wip: 0 };
  for (const m of text.matchAll(/^###\s+\[([ xX~])\]/gm)) {
    c.total++;
    if (m[1].toLowerCase() === 'x') c.done++;
    if (m[1] === '~') c.wip++;
  }
  return c;
}

export function waves(root) {
  const rows = tableRows(read(root, 'docs/04-plan/waves/README.md'));
  const out = rows.map((r) => {
    const link = r[4].match(/\(([^)]+)\)/);
    const src = link ? `docs/04-plan/waves/${link[1]}` : '';
    return { id: r[0], title: plain(r[1]), milestone: r[2], status: plain(r[3]).toLowerCase(), src, ...sliceCounts(read(root, src)) };
  });
  const next = out.find((w) => w.status !== 'done');
  if (next && next.status === 'planned') next.status = 'next';
  return out;
}

export function milestones(root, ws) {
  const text = read(root, 'docs/04-plan/ROADMAP.md');
  const table = text.slice(text.indexOf('| M |'));
  const status = (wave) => ws.find((w) => w.id === wave)?.status || 'planned';
  return tableRows(table.slice(0, table.indexOf('\n\n'))).map((r) => ({ id: r[0], name: r[1], demo: r[2], wave: r[3], status: status(r[3]) }));
}

// epics: checkbox counts where the backlog has them; otherwise the epic is
// done when every wave it names is done.
export function epics(root, ws) {
  const text = read(root, 'docs/04-plan/BACKLOG.md');
  const parts = text.split(/^## (?=E\d\d )/m).slice(1);
  return parts.map((p) => {
    const [head, ...rest] = p.split('\n');
    const body = rest.join('\n');
    const exit = (body.match(/^\*Exit:\s*(.*?)\*(.*)$/m) || []);
    const done = (body.match(/\[x\]/gi) || []).length;
    const open = (body.match(/\[ \]/g) || []).length;
    const named = [...`${exit[2] || ''}`.matchAll(/\bW(\d+)\b/g)].map((m) => `W${m[1]}`);
    const wavesDone = named.length > 0 && named.every((id) => ws.find((w) => w.id === id)?.status === 'done');
    const features = (body.match(/^- (\*\*)?E\d\d\.F\d\d/gm) || []).length;
    const id = head.slice(0, 3);
    return { id, title: head.slice(4).trim(), anchor: head.trim(), goal: (exit[1] || '').trim(), done, total: done + open, waves: named, wavesDone, features };
  });
}

export function devlog(root) {
  const text = read(root, 'docs/living/DEVLOG.md');
  return [...text.matchAll(/^## (\d{4}-\d\d-\d\d) — (.+)$/gm)].map((m) => ({ date: m[1], title: m[2].trim() }));
}

function countFiles(dir, re, pred) {
  let n = 0;
  const walk = (d) => {
    for (const e of fs.readdirSync(d, { withFileTypes: true })) {
      if (e.name === 'node_modules' || e.name === 'build') continue;
      const p = path.join(d, e.name);
      if (e.isDirectory()) walk(p);
      else if (re.test(e.name)) n += pred(fs.readFileSync(p, 'utf8'));
    }
  };
  if (fs.existsSync(dir)) walk(dir);
  return n;
}

export function quality(root) {
  const bugs = read(root, 'docs/living/BUGS_AND_MITIGATIONS.md');
  const section = (name) => {
    const i = bugs.indexOf(`\n## ${name}`);
    if (i < 0) return '';
    const j = bugs.indexOf('\n## ', i + 4);
    return bugs.slice(i, j < 0 ? undefined : j);
  };
  const count = (s, re) => (s.match(re) || []).length;
  const lines = (dir, ext, re) => countFiles(path.join(root, dir), ext, (t) => count(t, re));
  return {
    bugsOpen: count(section('Open'), /^### BUG-\d+/gm),
    bugsClosed: count(section('Closed'), /^### BUG-\d+/gm),
    pitfalls: count(bugs, /^\| PIT-\d+/gm),
    cTests: count(read(root, 'tests/unit/c/test_main.c'), /\bRUN\(/g),
    goTests: lines('kernel', /_test\.go$/, /^func Test/gm) + lines('tools', /_test\.go$/, /^func Test/gm),
    markers: lines('tests/smoke/expect', /\.txt$/, /^(?!\s*#)\S.*$/gm),
    ktests: lines('tests/ktest/expect', /\.txt$/, /^\[ktest\] /gm),
    regressions: count(read(root, 'tests/regression/run_all.sh'), /^\s*case_run REG-/gm),
    adrs: fs.readdirSync(path.join(root, 'docs/03-decisions')).filter((f) => /^ADR-(?!0000)\d{4}/.test(f)).length,
    diagrams: lines('docs', /\.md$/, /^```mermaid/gm),
  };
}

export function progress(root, date) {
  const ws = waves(root);
  const totals = ws.reduce((a, w) => ({ slicesDone: a.slicesDone + w.done, slices: a.slices + w.total }), { slicesDone: 0, slices: 0 });
  return { built: date, totals, waves: ws, milestones: milestones(root, ws), epics: epics(root, ws), devlog: devlog(root), quality: quality(root) };
}
