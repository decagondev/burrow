// home.mjs - the two pages computed from the plan rather than written in
// Markdown: the home page (the "strata" of layers with their progress) and
// the Progress page (milestones, waves, epics, quality signals). Every number
// comes from plan.mjs; nothing here is typed in by hand except the layers.
// Docs: docs/06-guides/PUBLISHING_THE_SITE.md.
import { esc, codeBlock } from './md.mjs';

// The layers from the top of the stack down, and the waves that build each.
// Add each new wave here when it starts, or its layer card never shows it
// (a wave only appears on the home page through this list).
const STRATA = [
  { cls: 's-desk', name: 'Desktop', lang: 'C++, C + musl', waves: ['W14', 'W15', 'W16'], text: 'A mouse, Unix sockets and file mappings below toolkits; then Qt widgets on the framebuffer and an X11 desktop with dwm.' },
  { cls: 's-user', name: 'User space', lang: 'C, C++, Go, Lua + musl', waves: ['W9', 'W10', 'W11', 'W12', 'W13', 'W19', 'W24'], text: 'Shell, busybox, DOOM, compilers and editors talking to the kernel through the Linux system-call ABI; users, logins and passwords; ssh and a browser.' },
  { cls: 's-go', name: 'Go kernel', lang: 'Go', waves: ['W6', 'W7', 'W8', 'W17', 'W18', 'W22', 'W23', 'W25'], text: 'The stock Go runtime in ring 0: drivers (sound, virtio, an absolute pointer), files (FAT32, ext2), processes with copy-on-write fork and shared memory, POSIX policy and a TCP/IP stack.' },
  { cls: 's-nucleus', name: 'Nucleus', lang: 'C + assembly', waves: ['W4', 'W5', 'W20'], text: 'Threads on every CPU, memory, interrupts and the system-call gate that lets Go run, with the CPU\'s own protections on.' },
  { cls: 's-loader', name: 'C loader', lang: 'C', waves: ['W3', 'W26'], text: 'Verifies every input, reads the disk and hands over a description of the machine, from a BIOS or from UEFI.' },
  { cls: 's-stage2', name: 'Stage 2', lang: 'NASM', waves: ['W2', 'W21'], text: 'Memory map, A20, protected mode, paging, long mode, then calls C; a VBE graphics mode from the BIOS.' },
  { cls: 's-stage1', name: 'Stage 1', lang: 'NASM', waves: ['W1'], text: 'The 512-byte boot sector the BIOS runs first.' },
  { cls: 's-tools', name: 'Foundations', lang: 'Make, Go, shell', waves: ['W0'], text: 'Build system, disk-image tool and the test harness every layer stands on.' },
];

const PILL = { done: 'Done', next: 'Next', planned: 'Planned' };
export const pill = (st) => `<span class="pill ${st === 'done' ? 'done' : st === 'next' ? 'next' : 'planned'}">${PILL[st] || 'Planned'}</span>`;
export function bar(done, total, label) {
  const pct = total ? Math.round((100 * done) / total) : 0;
  return `<div class="bar" role="progressbar" aria-valuemin="0" aria-valuemax="${total}" aria-valuenow="${done}" aria-label="${esc(label)}"><span style="width:${pct}%"></span></div>`;
}
const waveUrl = (w) => w.src.replace(/^docs\//, 'docs/').replace(/\.md$/, '.html');
const md = (s) => esc(s).replace(/`([^`]+)`/g, '<code>$1</code>');

function stratum(s, ws) {
  const mine = ws.filter((w) => s.waves.includes(w.id));
  const done = mine.reduce((a, w) => a + w.done, 0);
  const total = mine.reduce((a, w) => a + w.total, 0);
  const st = mine.every((w) => w.status === 'done') ? 'done' : mine.some((w) => w.status === 'next' || w.done > 0) ? 'next' : 'planned';
  const here = st === 'next';
  const links = mine.map((w) => `<a href="${waveUrl(w)}">${w.id}</a>`).join(', ');
  return `<li class="stratum ${s.cls} st-${st}${here ? ' here' : ''}">
  <div class="stratum-soil" aria-hidden="true"></div>
  <div class="stratum-body">
    <div class="stratum-title"><h3>${esc(s.name)}</h3><span class="lang">${esc(s.lang)}</span>${here ? '<span class="here-tag">Working here now</span>' : ''}</div>
    <p>${esc(s.text)}</p>
    <div class="stratum-prog">${bar(done, total, `${s.name} progress`)}<span>${done}/${total} slices</span></div>
  </div>
  <div class="stratum-side">${pill(st)}<span class="waves">Wave${mine.length > 1 ? 's' : ''} ${links}</span></div>
</li>`;
}

export function homeBody(pr, shots = []) {
  const q = pr.quality;
  const shotWaves = [...new Set(shots.map((s) => s.wave).filter(Boolean))];
  const gallery = shots.length
    ? `<div><dt>Screenshots</dt><dd><a href="gallery.html">${shots.length} screens from the smoke tests</a>, ${shotWaves[0]} to ${shotWaves[shotWaves.length - 1]}</dd></div>`
    : '';
  const cur = pr.waves.find((w) => w.status !== 'done') || pr.waves[pr.waves.length - 1];
  const last = pr.devlog[0] || { title: '', date: '' };
  const strata = STRATA.map((s) => stratum(s, pr.waves)).join('\n');
  return `
<section class="hero">
  <div class="hero-text">
    <h1>Burrow</h1>
    <p class="lede">An educational x86-64 operating system. Our own assembly boots the machine, C prepares it, and a kernel written in Go runs on top: a POSIX-like system that runs DOOM, compiles C, runs an X11 desktop, talks to the network, logs users in, and runs on every CPU of the machine.</p>
    <p class="hero-actions"><a class="btn primary" href="docs/02-architecture/ARCHITECTURE.html">Read the architecture</a><a class="btn" href="progress.html">See progress</a>${shots.length ? '<a class="btn" href="gallery.html">See screenshots</a>' : ''}</p>
  </div>
  <div class="strata-wrap">
    <p class="strata-caption">Each layer is built on the one beneath it. Work starts at the bottom and digs upward toward user space.</p>
    <ol class="strata" reversed>
      <li class="ground" aria-hidden="true"><span>Applications</span></li>
${strata}
      <li class="bedrock"><span>Hardware: x86-64 PC, emulated by QEMU</span></li>
    </ol>
  </div>
</section>

<section class="home-section status-now">
  <h2>Where things stand</h2>
  <dl class="facts">
    <div><dt>Current wave</dt><dd><a href="${waveUrl(cur)}">${cur.id}: ${esc(cur.title)}</a></dd></div>
    <div><dt>Slices completed</dt><dd>${pr.totals.slicesDone} of ${pr.totals.slices} planned in detail</dd></div>
    <div><dt>Latest devlog entry</dt><dd><a href="docs/living/DEVLOG.html">${esc(last.title)}</a> <span class="muted">(${last.date})</span></dd></div>
    <div><dt>Bugs</dt><dd><a href="docs/living/BUGS_AND_MITIGATIONS.html">${q.bugsOpen} open, ${q.bugsClosed} closed</a>, ${q.pitfalls} pitfalls anticipated</dd></div>
    <div><dt>Automated tests</dt><dd>${q.cTests} C unit tests, ${q.goTests} Go tests, ${q.ktests} in-kernel tests, ${q.markers} boot markers, ${q.regressions} regression cases</dd></div>
    <div><dt>Decisions recorded</dt><dd><a href="docs/03-decisions/index.html">${q.adrs} architecture decision records</a></dd></div>
    ${gallery}
  </dl>
</section>

<section class="home-section">
  <h2>Reading path for newcomers</h2>
  <ol class="path-list">
    <li><a href="docs/00-vision/VISION.html">Vision</a><span>What Burrow is for and what it deliberately is not.</span></li>
    <li><a href="docs/02-architecture/ARCHITECTURE.html">Architecture</a><span>The layers, the boot sequence and how data moves, in diagrams.</span></li>
    <li><a href="docs/04-plan/PLAN_OVERVIEW.html">How the plan works</a><span>Epics, stories, slices and waves, and when a slice counts as done.</span></li>
    <li><a href="docs/06-guides/WORKFLOW.html">Working a slice</a><span>The loop you follow every session, with or without an assistant.</span></li>
    <li><a href="docs/06-guides/WORKBENCH.html">Working on Burrow</a><span>Edit, compile and run code inside Burrow itself.</span></li>
    <li><a href="${waveUrl(cur)}">The current wave</a><span>Pick the next open slice and start typing.</span></li>
  </ol>
</section>

<section class="home-section">
  <h2>Build and boot it</h2>
  ${codeBlock(`make          # build build/disk.img
make test     # unit tests, QEMU boots, smoke and regression suites
make run-go   # boot the Go kernel in QEMU, serial output in the terminal
make doom && make run-doom   # download (pinned) and play Freedoom
make site     # rebuild this website from the docs`, 'sh')}
  <p>Toolchain details are in the <a href="docs/06-guides/TOOLCHAIN_SETUP.html">setup guide</a>.</p>
</section>`;
}

function epicCell(e) {
  if (e.total) return `<td class="bar-cell">${bar(e.done, e.total, `${e.id} progress`)}<span>${e.done}/${e.total}</span></td>`;
  if (e.wavesDone) return `<td class="bar-cell">${pill('done')} <span class="muted">${e.waves.join(', ')}</span></td>`;
  return `<td class="bar-cell"><span class="muted">${e.features ? `${e.features} features planned` : 'Planned'}${e.waves.length ? ` (${e.waves.join(', ')})` : ''}</span></td>`;
}

export function progressBody(pr, slug) {
  const q = pr.quality;
  const pct = pr.totals.slices ? Math.round((100 * pr.totals.slicesDone) / pr.totals.slices) : 0;
  const ms = pr.milestones.map((m) => `<li class="ms st-${m.status === 'done' ? 'done' : m.status === 'next' ? 'next' : 'planned'}"><span class="ms-dot" aria-hidden="true"></span><div><p class="ms-name"><strong>${esc(m.id)}</strong> ${esc(m.name)}</p><p class="ms-demo">${md(m.demo)}</p></div><span class="ms-wave">${esc(m.wave)}</span></li>`).join('');
  const wr = pr.waves.map((w) => `<tr><td><a href="${waveUrl(w)}">${w.id}</a></td><td>${esc(w.title)}</td><td>${esc(w.milestone)}</td><td>${pill(w.status)}</td><td class="bar-cell">${bar(w.done, w.total, `${w.id} progress`)}<span>${w.done}/${w.total}</span></td></tr>`).join('');
  const er = pr.epics.map((e) => `<tr><td>${e.id}</td><td><a href="docs/04-plan/BACKLOG.html#${slug(e.anchor)}">${esc(e.title)}</a><small>${md(e.goal)}</small></td>${epicCell(e)}</tr>`).join('');
  const row = (k, v) => `<tr><td>${k}</td><td>${v}</td></tr>`;
  return `<p class="crumbs">Project status</p><h1>Progress</h1>
<p class="lede">Computed from the plan's checkboxes each time the site is built. To update it, tick slices in the wave files and run <code>make site</code>.</p>
<div class="overall">
  <div><p class="overall-num">${pct}%</p><p>of the ${pr.totals.slices} slices planned in detail are done</p></div>
  ${bar(pr.totals.slicesDone, pr.totals.slices, 'Overall progress')}
</div>
<h2 id="milestones">Milestones</h2>
<ol class="milestones">${ms}</ol>
<h2 id="waves">Waves</h2>
<div class="table-wrap wide"><table class="progress-table"><thead><tr><th>Wave</th><th>Title</th><th>Milestone</th><th>Status</th><th>Slices</th></tr></thead><tbody>${wr}</tbody></table></div>
<h2 id="epics">Epics</h2>
<div class="table-wrap wide"><table class="progress-table epics"><thead><tr><th>Epic</th><th>Goal</th><th>Stories</th></tr></thead><tbody>${er}</tbody></table></div>
<h2 id="quality">Quality signals</h2>
<div class="table-wrap"><table><tbody>
${row('Bugs open / closed', `<a href="docs/living/BUGS_AND_MITIGATIONS.html">${q.bugsOpen} / ${q.bugsClosed}</a>`)}
${row('Anticipated pitfalls', q.pitfalls)}
${row('C unit tests', q.cTests)}
${row('Go tests', q.goTests)}
${row('In-kernel tests (ktest)', q.ktests)}
${row('Ordered boot markers', q.markers)}
${row('Regression cases', q.regressions)}
${row('Architecture decisions', q.adrs)}
${row('Diagrams in the docs', q.diagrams)}
</tbody></table></div>`;
}
