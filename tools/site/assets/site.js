/* site.js - behaviour for the Burrow docs site. No frameworks, no network.
 * Sections: theme, navigation drawer, heading anchors + TOC, copy buttons,
 * search, diagrams (Mermaid, re-themed live), diagram viewer (pan/zoom). */
(function () {
  'use strict';
  const $ = (s, r = document) => r.querySelector(s);
  const $$ = (s, r = document) => Array.from(r.querySelectorAll(s));
  const root = document.documentElement;
  const ROOT = window.SITE_ROOT || './';

  /* ------------------------------------------------------------ theme */
  const THEME_KEY = 'burrow-theme';
  const ORDER = ['system', 'light', 'dark'];
  const LABEL = { system: 'Theme: match system', light: 'Theme: light', dark: 'Theme: dark' };
  const media = matchMedia('(prefers-color-scheme: dark)');
  const getPref = () => { try { return localStorage.getItem(THEME_KEY) || 'system'; } catch (e) { return 'system'; } };
  function applyTheme(pref) {
    const dark = pref === 'dark' || (pref === 'system' && media.matches);
    const next = dark ? 'dark' : 'light';
    const changed = root.dataset.theme !== next;
    root.dataset.theme = next;
    root.dataset.themePref = pref;
    const btn = $('.theme-toggle');
    if (btn) { btn.setAttribute('aria-label', `${LABEL[pref]}. Click to change.`); btn.title = LABEL[pref]; }
    if (changed) document.dispatchEvent(new CustomEvent('themechange'));
  }
  applyTheme(getPref());
  $('.theme-toggle')?.addEventListener('click', () => {
    const pref = ORDER[(ORDER.indexOf(getPref()) + 1) % ORDER.length];
    try { localStorage.setItem(THEME_KEY, pref); } catch (e) { /* private mode: session only */ }
    applyTheme(pref);
  });
  media.addEventListener?.('change', () => { if (getPref() === 'system') applyTheme('system'); });

  /* ------------------------------------------------------- nav drawer */
  const navBtn = $('.nav-toggle');
  const scrim = $('.scrim');
  function setNav(open) {
    document.body.classList.toggle('nav-open', open);
    navBtn?.setAttribute('aria-expanded', String(open));
    navBtn?.setAttribute('aria-label', open ? 'Close navigation' : 'Open navigation');
    if (scrim) scrim.hidden = !open;
  }
  navBtn?.addEventListener('click', () => setNav(!document.body.classList.contains('nav-open')));
  scrim?.addEventListener('click', () => setNav(false));
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && document.body.classList.contains('nav-open')) setNav(false); });
  $('.sidebar [aria-current="page"]')?.scrollIntoView({ block: 'center' });

  /* ------------------------------------------- heading anchors + TOC */
  $$('.prose h2[id], .prose h3[id], .prose h4[id]').forEach((h) => {
    if (h.closest('.entry') && h.tagName === 'H2' && document.body.classList.contains('kind-devlog')) return;
    const a = document.createElement('a');
    a.className = 'heading-anchor';
    a.href = `#${h.id}`;
    a.setAttribute('aria-label', `Link to this section: ${h.textContent.trim()}`);
    a.textContent = '#';
    h.prepend(a);
  });
  const tocLinks = $$('.toc a');
  if (tocLinks.length && 'IntersectionObserver' in window) {
    const byId = new Map(tocLinks.map((a) => [decodeURIComponent(a.hash.slice(1)), a]));
    const visible = new Set();
    const obs = new IntersectionObserver((entries) => {
      entries.forEach((en) => (en.isIntersecting ? visible.add(en.target.id) : visible.delete(en.target.id)));
      const heads = $$('.prose h2[id], .prose h3[id]').filter((h) => byId.has(h.id));
      let current = heads.find((h) => visible.has(h.id));
      if (!current) current = heads.filter((h) => h.getBoundingClientRect().top < 120).pop();
      tocLinks.forEach((a) => a.classList.remove('active'));
      if (current) byId.get(current.id)?.classList.add('active');
    }, { rootMargin: '-64px 0px -60% 0px' });
    $$('.prose h2[id], .prose h3[id]').forEach((h) => obs.observe(h));
  }

  /* ----------------------------------------------------- copy buttons */
  async function copyText(text, btn) {
    try { await navigator.clipboard.writeText(text); }
    catch (e) {
      const ta = document.createElement('textarea');
      ta.value = text; document.body.append(ta); ta.select();
      try { document.execCommand('copy'); } catch (_) { /* ignore */ }
      ta.remove();
    }
    const old = btn.textContent;
    btn.textContent = 'Copied';
    setTimeout(() => { btn.textContent = old; }, 1400);
  }
  $$('.code-block').forEach((block) => {
    const head = $('.code-head', block);
    const code = $('pre:not(.gutter) code', block);
    if (!head || !code) return;
    const btn = document.createElement('button');
    btn.type = 'button'; btn.className = 'copy-btn'; btn.textContent = 'Copy';
    btn.setAttribute('aria-label', 'Copy code to clipboard');
    btn.addEventListener('click', () => copyText(code.innerText, btn));
    head.append(btn);
  });

  /* ----------------------------------------------------------- search */
  const dlg = $('.search-dialog');
  const input = dlg && $('input', dlg);
  const list = dlg && $('.search-results', dlg);
  let results = [];
  let sel = 0;
  const escHtml = (s) => s.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
  const mark = (text, terms) => {
    let h = escHtml(text);
    terms.forEach((t) => { h = h.replace(new RegExp(`(${t.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'ig'), '<mark>$1</mark>'); });
    return h;
  };
  function search(q) {
    const terms = q.toLowerCase().split(/\s+/).filter((t) => t.length > 1);
    if (!terms.length) return [];
    const out = [];
    for (const p of window.SEARCH_INDEX || []) {
      const title = p.t.toLowerCase();
      const text = p.x.toLowerCase();
      let score = 0;
      let all = true;
      for (const t of terms) {
        const inTitle = title.includes(t);
        const inHead = p.h.some(([h]) => h.toLowerCase().includes(t));
        const inText = text.includes(t);
        if (!inTitle && !inHead && !inText) { all = false; break; }
        score += (inTitle ? 12 : 0) + (inHead ? 5 : 0) + (inText ? 1 : 0);
      }
      if (!all) continue;
      if (p.g === 'Source code') score -= 3;
      const head = p.h.find(([h]) => terms.every((t) => h.toLowerCase().includes(t)));
      let snip = '';
      const i = text.indexOf(terms[0]);
      if (i >= 0) snip = (i > 40 ? '…' : '') + p.x.slice(Math.max(0, i - 40), i + 110).trim();
      out.push({ p, score: score + (head ? 6 : 0), head, snip });
    }
    return out.sort((a, b) => b.score - a.score).slice(0, 12).map((r) => ({ ...r, terms }));
  }
  function renderResults() {
    list.innerHTML = results.map((r, i) => {
      const url = ROOT + r.p.u + (r.head ? `#${r.head[1]}` : '');
      const title = r.head ? `${r.p.t}: ${r.head[0]}` : r.p.t;
      return `<li role="option" id="sr-${i}" aria-selected="${i === sel}"><a href="${url}"><span class="r-title">${mark(title, r.terms)}</span><span class="r-group">${escHtml(r.p.g || '')}</span>${r.snip ? `<span class="r-snip">${mark(r.snip, r.terms)}</span>` : ''}</a></li>`;
    }).join('');
    const hint = $('.search-hint', dlg);
    if (input.value.trim().length > 1 && !results.length) hint.textContent = `No pages match "${input.value.trim()}". Try fewer or shorter words.`;
    else hint.textContent = 'Type at least two letters. Use the arrow keys and Enter to open a result.';
    input.setAttribute('aria-activedescendant', results.length ? `sr-${sel}` : '');
  }
  function openSearch() {
    if (!dlg) return;
    setNav(false);
    dlg.showModal();
    input.select();
  }
  $('.search-btn')?.addEventListener('click', openSearch);
  document.addEventListener('keydown', (e) => {
    const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName || '') || document.activeElement?.isContentEditable;
    if ((e.key === '/' && !typing) || ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k')) {
      if (!dlg.open) { e.preventDefault(); openSearch(); }
    }
  });
  input?.addEventListener('input', () => { sel = 0; results = search(input.value); renderResults(); });
  input?.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!results.length) return;
      sel = (sel + (e.key === 'ArrowDown' ? 1 : -1) + results.length) % results.length;
      renderResults();
      $(`#sr-${sel}`, list)?.scrollIntoView({ block: 'nearest' });
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const a = $(`#sr-${sel} a`, list);
      if (a) location.href = a.href;
    }
  });
  dlg?.addEventListener('click', (e) => { if (e.target === dlg) dlg.close(); });

  /* --------------------------------------------------------- diagrams */
  const figures = $$('figure[data-diagram]');
  let renderSeq = 0;
  const cssVar = (n) => getComputedStyle(root).getPropertyValue(n).trim();

  function mermaidConfig() {
    const v = cssVar;
    return {
      startOnLoad: false,
      securityLevel: 'strict',
      theme: 'base',
      fontFamily: v('--font-body'),
      themeVariables: {
        fontFamily: v('--font-body'),
        fontSize: '15px',
        background: v('--surface'),
        primaryColor: v('--dg-node'),
        primaryTextColor: v('--text'),
        primaryBorderColor: v('--dg-border'),
        secondaryColor: v('--dg-node2'),
        secondaryTextColor: v('--text'),
        secondaryBorderColor: v('--dg-border'),
        tertiaryColor: v('--dg-cluster'),
        tertiaryTextColor: v('--text'),
        tertiaryBorderColor: v('--dg-border'),
        lineColor: v('--dg-line'),
        textColor: v('--text'),
        mainBkg: v('--dg-node'),
        nodeBorder: v('--dg-border'),
        clusterBkg: v('--dg-cluster'),
        clusterBorder: v('--dg-border'),
        titleColor: v('--text'),
        edgeLabelBackground: v('--surface'),
        noteBkgColor: v('--dg-note'),
        noteTextColor: v('--text'),
        noteBorderColor: v('--dg-border'),
        actorBkg: v('--dg-node'),
        actorBorder: v('--dg-border'),
        actorTextColor: v('--text'),
        actorLineColor: v('--dg-line'),
        signalColor: v('--text'),
        signalTextColor: v('--text'),
        labelBoxBkgColor: v('--dg-node2'),
        labelBoxBorderColor: v('--dg-border'),
        labelTextColor: v('--text'),
        loopTextColor: v('--text'),
        activationBkgColor: v('--dg-node2'),
        activationBorderColor: v('--dg-border'),
        sequenceNumberColor: v('--surface'),
        classText: v('--text'),
        labelColor: v('--text'),
        stateLabelColor: v('--text'),
        transitionColor: v('--dg-line'),
        transitionLabelColor: v('--text'),
        compositeBackground: v('--dg-cluster'),
        compositeTitleBackground: v('--dg-node2'),
        altBackground: v('--dg-cluster'),
        specialStateColor: v('--dg-line'),
      },
      flowchart: { htmlLabels: true, curve: 'basis', padding: 14, nodeSpacing: 46, rankSpacing: 52, useMaxWidth: false },
      sequence: { useMaxWidth: false, mirrorActors: false, actorMargin: 60, messageMargin: 38, boxMargin: 8, noteMargin: 12, wrap: true, width: 170 },
      state: { useMaxWidth: false },
      class: { useMaxWidth: false },
    };
  }

  function buildFigureChrome(fig, i) {
    const src = $('.diagram-src code', fig).textContent;
    fig.dataset.source = src;
    const heading = (() => {
      let el = fig.previousElementSibling;
      while (el && !/^H[1-4]$/.test(el.tagName)) el = el.previousElementSibling;
      return el ? el.textContent.replace(/^#/, '').trim() : `Diagram ${i + 1}`;
    })();
    fig.dataset.title = heading;
    const bar = document.createElement('div');
    bar.className = 'diagram-bar';
    bar.innerHTML = '<button type="button" data-act="expand">Expand</button><button type="button" data-act="source" aria-pressed="false">Source</button><button type="button" data-act="copy">Copy source</button>';
    const view = document.createElement('div');
    view.className = 'diagram-view';
    view.setAttribute('role', 'img');
    view.setAttribute('aria-label', `Diagram: ${heading}. The source is available with the Source button.`);
    fig.prepend(bar, view);
    bar.addEventListener('click', (e) => {
      const act = e.target.closest('button')?.dataset.act;
      if (act === 'expand') openViewer(fig);
      if (act === 'source') {
        const on = fig.classList.toggle('show-src');
        e.target.setAttribute('aria-pressed', String(on));
      }
      if (act === 'copy') copyText(fig.dataset.source, e.target);
    });
    view.addEventListener('dblclick', () => openViewer(fig));
  }

  // Natural size from the viewBox attribute (works in every browser; Firefox
  // returns null from viewBox.baseVal when the attribute is missing).
  function svgSize(svg) {
    const vb = (svg.getAttribute('viewBox') || '').trim().split(/[\s,]+/).map(Number);
    if (vb.length === 4 && vb[2] > 0 && vb[3] > 0) return { w: vb[2], h: vb[3] };
    const w = parseFloat(svg.getAttribute('width')) || parseFloat(svg.style.maxWidth) || 800;
    const h = parseFloat(svg.getAttribute('height')) || 600;
    return { w, h };
  }

  function fitSvg(fig) {
    const view = $('.diagram-view', fig);
    const svg = $('svg', view);
    if (!svg) return;
    const natural = svgSize(svg).w;
    const avail = view.clientWidth - 40;
    const minScale = 0.72;                                   // never shrink text below ~72%
    const w = natural <= avail ? natural : Math.max(avail, natural * minScale);
    svg.style.width = `${w}px`;
    svg.style.maxWidth = 'none';
    svg.removeAttribute('height');
    view.style.justifyContent = w > avail ? 'flex-start' : 'center';
  }

  async function renderAll() {
    if (!window.mermaid || !figures.length) return;
    try { await document.fonts?.ready; } catch (e) { /* ignore */ }
    window.mermaid.initialize(mermaidConfig());
    for (const fig of figures) {
      const view = $('.diagram-view', fig);
      try {
        const { svg } = await window.mermaid.render(`dg-${++renderSeq}`, fig.dataset.source);
        view.innerHTML = svg;
        fig.dataset.state = 'rendered';
      } catch (err) {
        fig.dataset.state = 'error';
        view.innerHTML = `<p class="diagram-error">This diagram could not be drawn. Its source is shown below. (${escHtml(String(err.message || err).split('\n')[0])})</p>`;
        continue;
      }
      try { fitSvg(fig); } catch (e) { /* sizing is cosmetic; keep the diagram */ }
    }
  }
  figures.forEach(buildFigureChrome);
  if (figures.length) {
    const start = () => renderAll();
    if (window.mermaid) start(); else window.addEventListener('load', start);
    document.addEventListener('themechange', () => renderAll());
    let t;
    window.addEventListener('resize', () => { clearTimeout(t); t = setTimeout(() => figures.forEach(fitSvg), 150); });
  }

  /* ---------------------------------------------- diagram viewer */
  const lb = $('.lightbox');
  const stage = lb && $('.lb-stage', lb);
  const canvas = lb && $('.lb-canvas', lb);
  const state = { s: 1, x: 0, y: 0, w: 0, h: 0 };
  const applyT = () => { canvas.style.transform = `translate(${state.x}px, ${state.y}px) scale(${state.s})`; };
  function fit() {
    const r = stage.getBoundingClientRect();
    state.s = Math.min((r.width - 48) / state.w, (r.height - 48) / state.h, 2.5);
    state.x = (r.width - state.w * state.s) / 2;
    state.y = (r.height - state.h * state.s) / 2;
    applyT();
  }
  function zoomAt(factor, cx, cy) {
    const ns = Math.min(8, Math.max(0.15, state.s * factor));
    state.x = cx - ((cx - state.x) * ns) / state.s;
    state.y = cy - ((cy - state.y) * ns) / state.s;
    state.s = ns;
    applyT();
  }
  function openViewer(fig) {
    const svg = $('.diagram-view svg', fig);
    if (!svg || !lb) return;
    const clone = svg.cloneNode(true);
    ({ w: state.w, h: state.h } = svgSize(svg));
    clone.style.width = `${state.w}px`;
    clone.style.height = `${state.h}px`;
    canvas.replaceChildren(clone);
    $('.lb-title', lb).textContent = fig.dataset.title || 'Diagram';
    lb.showModal();
    requestAnimationFrame(fit);
  }
  if (lb) {
    $('.lb-tools', lb).addEventListener('click', (e) => {
      const z = e.target.closest('button')?.dataset.zoom;
      const r = stage.getBoundingClientRect();
      if (z === 'in') zoomAt(1.25, r.width / 2, r.height / 2);
      if (z === 'out') zoomAt(0.8, r.width / 2, r.height / 2);
      if (z === 'fit') fit();
      if (z === 'close') lb.close();
    });
    stage.addEventListener('wheel', (e) => {
      e.preventDefault();
      const r = stage.getBoundingClientRect();
      zoomAt(e.deltaY < 0 ? 1.12 : 0.89, e.clientX - r.left, e.clientY - r.top);
    }, { passive: false });
    let drag = null;
    stage.addEventListener('pointerdown', (e) => { drag = { x: e.clientX - state.x, y: e.clientY - state.y }; stage.setPointerCapture(e.pointerId); stage.classList.add('dragging'); });
    stage.addEventListener('pointermove', (e) => { if (!drag) return; state.x = e.clientX - drag.x; state.y = e.clientY - drag.y; applyT(); });
    const end = () => { drag = null; stage.classList.remove('dragging'); };
    stage.addEventListener('pointerup', end);
    stage.addEventListener('pointercancel', end);
    lb.addEventListener('keydown', (e) => {
      const r = stage.getBoundingClientRect();
      if (e.key === '+' || e.key === '=') zoomAt(1.25, r.width / 2, r.height / 2);
      if (e.key === '-') zoomAt(0.8, r.width / 2, r.height / 2);
      if (e.key === '0') fit();
    });
    window.addEventListener('resize', () => { if (lb.open) fit(); });
  }
}());
