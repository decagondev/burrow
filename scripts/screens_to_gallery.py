#!/usr/bin/env python3
"""screens_to_gallery.py - turn the smoke tests' screendumps into the docs
site's screenshot gallery: every `screendump build/screen-<name>.ppm` that a
tests/smoke/keys/*.keys file names becomes docs/gallery/<name>.png (full
size) and docs/gallery/thumbs/<name>.png (half size), and
docs/gallery/gallery.json describes each one: a caption, what its .screen
file checks, the test that takes it, and the slice that introduced it (the
first commit whose diff adds the dump's name; its subject starts with the
slice id), with the wave and a link to the slice's heading.

It never runs QEMU or a test and never writes outside docs/gallery/. A dump
that is missing or half written (a test may be running) keeps its last PNG;
a slice it cannot find in git is left out, never guessed (the wave then comes
from the keys file's header comment). Rerun it after any test run.

Usage: python3 scripts/screens_to_gallery.py   (from anywhere; stdlib only)
Docs: docs/06-guides/PUBLISHING_THE_SITE.md ("Keep it current").
"""
import glob
import json
import os
import re
import struct
import subprocess
import sys
import time
import unicodedata
import zlib

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), '..'))
KEYS_GLOB = 'tests/smoke/keys/*.keys'
EXPECT_DIR = 'tests/smoke/expect'
WAVES_DIR = 'docs/04-plan/waves'
OUT_DIR = 'docs/gallery'
THUMB_DIR = 'docs/gallery/thumbs'
MANIFEST = 'docs/gallery/gallery.json'
SCREEN_W, SCREEN_H = 1024, 768          # what every dump in the suite is
RETRIES, RETRY_WAIT_S = 3, 30           # a dump mid-write gets this long
DUMP_RE = re.compile(r'^screendump\s+build/(screen-([\w.-]+))\.ppm\s*$')
SLICE_RE = re.compile(r'^(W(\d+)-S(\d+))(?:-S(\d+))?\b')

# Captions for the gallery, written from each dump's .screen comments (which
# list pixel checks rather than describe the picture). A dump not listed
# here gets its caption from those comments as they are.
CAPTIONS = {
    'boot_to_go': "The Go kernel's console at its first prompt: the gradient band with the Burrow title, drawn in the hand-made bitmap font on the BGA framebuffer.",
    'boot_to_go-end': 'The same console at the end of the session typed into it over the PS/2 keyboard: signal tests, ttytest, a pipeline in sh, ^C on a spinning program, and back at the prompt.',
    'busybox-vi': 'busybox vi editing a file on the screen console. The console understood the VT100 sequences vi sent: rows addressed directly, tildes, a status line and a cursor.',
    'desk-paint': "paint takes the screen the way an X server would (KD_GRAPHICS, the keyboard off): a palette along the top and a red stroke dragged with the PS/2 mouse.",
    'desk-back': 'After Esc, paint hands the screen back (KD_TEXT) and the console repaints its band and paper.',
    'desk-crash': 'paint crashed while it held the screen; the kernel gave the screen back to the console by itself.',
    'desktop-doom': "The finished desktop: Doom started from dmenu, floating over two terminals in dwm's focused border.",
    'desktop': "After Quit Game: dwm's bar, the two tiled terminals, and the master terminal focused.",
    'desktop-back': 'Alt+Shift+q ends the session: startx stops the X server and the console is back.',
    'dev-kilo': "kilo editing hello.c on the screen console, with C syntax colours (SGR 31-36) and a status bar in reverse video.",
    'devices-fb0': 'fbtest holds /dev/fb0 and paints eight colour bars; the console draws nothing over them while it is open.',
    'devices-after': 'fbtest closed /dev/fb0 and the console repainted its band and paper.',
    'devices-mouse': 'mousetest reads the PS/2 mouse through /dev/input and moves its small white cursor away from the centre of the screen.',
    'doom-title': "Doom (doomgeneric with Freedoom) on /dev/fb0: its title picture, centred on a screen the console cleared to black.",
    'doom-e1m1': 'E1M1 after New Game, with the status bar at the bottom of the frame.',
    'qt-window': 'qtwindow, a Qt 6 program drawing through linuxfb: three rectangles and text set in DejaVu Sans by FreeType.',
    'qt-after': 'After the Qt program quit, the console repainted its band and paper.',
    'qt-crash': 'A Qt program aborted with the screen taken; the kernel gave the console back.',
    'qt-demo': 'qtdemo, Qt widgets driven by keyboard and mouse: two lines added to the list, and the File menu opened with a click.',
    'qtpad': 'qtpad, a Qt editor, editing /home/hello.c: a line-number gutter, red keywords and the grey comment line typed during the test.',
    'x11-xhello': "Xfbdev, a TinyX server, on /dev/fb0 with its first client, xhello: three rectangles and Xft text over X's root weave.",
    'x11-after': 'The X server exited and the console is back.',
    'x11-st': 'st, the suckless terminal, running busybox sh on a pseudo-terminal, after a line was typed into it.',
    'x11-dwm': "dwm tiling two terminals, the focused one as master, with tag 1 and the title in dwm's bar.",
    'x11-dwm-tag2': 'Tag 2 selected in the bar: nothing on it but the root weave.',
    'x11-doom-title': "Doom started from dmenu, floating at its fixed 640x400 inside dwm's focused border: the title.",
    'x11-doom-e1m1': 'Doom in its window: E1M1 with the status bar.',
    'x11-doom-gone': 'After Quit Game the terminals are back, with the focus on the one it was on before.',
}


def git(*args):
    return subprocess.run(['git', *args], cwd=ROOT, capture_output=True, text=True, check=True).stdout


def slug(text):
    """GitHub's heading anchor, as tools/site/lib/md.mjs computes it."""
    keep = (c for c in text.strip().lower() if c.isalnum() or c in ' _-' or c.isspace() or unicodedata.category(c).startswith('M'))
    return re.sub(r'\s', '-', ''.join(keep))


def sentences(lines):
    """Comment lines joined into prose, without the files' boilerplate."""
    text = ' '.join(l.strip() for l in lines)
    text = re.sub(r'\s*Append-only\.\s*', ' ', text)
    text = re.sub(r'\s+', ' ', text).strip()
    text = re.sub(r'^S\d+:\s*', '', text)
    if text and text[-1] not in '.)':
        text += '.'
    if re.match(r'(the|a|an|after|while|its|tag)\b', text):   # program names keep their case
        text = text[:1].upper() + text[1:]
    return text


def keys_dumps():
    """[(test, keys path, dump name, wave from the header)] in file order."""
    found = []
    for path in sorted(glob.glob(os.path.join(ROOT, KEYS_GLOB))):
        rel = os.path.relpath(path, ROOT).replace(os.sep, '/')
        lines = open(path, encoding='utf-8').read().splitlines()
        header = ' '.join(l for l in lines if l.startswith('#'))
        m = re.search(r'\bW(\d+)\b', header)
        for line in lines:
            d = DUMP_RE.match(line.strip())
            if d:
                found.append({'test': os.path.basename(path)[:-5], 'keys': rel, 'dump': d.group(1), 'name': d.group(2),
                              'header_wave': f'W{m.group(1)}' if m else None})
    return found


def screen_notes():
    """dump name -> {'screen': file, 'shows': text of the comments above its
    size line, 'checks': [text of each comment group among its pixel lines]}.
    The file's header (up to "Append-only.") says only which file it is."""
    notes = {}
    for path in sorted(glob.glob(os.path.join(ROOT, EXPECT_DIR, '*.screen'))):
        rel = os.path.relpath(path, ROOT).replace(os.sep, '/')
        pending, cur, first = [], None, True
        for line in open(path, encoding='utf-8').read().splitlines():
            s = line.strip()
            if s.startswith('#'):
                pending.append(s.lstrip('#').strip())
                continue
            word = s.split()
            if not word:
                continue
            if word[0] == 'size' and len(word) > 1:
                text = ' '.join(pending)
                if first:
                    text = text.split('Append-only.', 1)[-1] if 'Append-only.' in text else ''
                    first = False
                name = re.sub(r'^build/screen-|\.ppm$', '', word[1])
                shows = sentences([text]) or (cur['shows'] if cur else '')   # several dumps under one comment
                cur = notes.setdefault(name, {'screen': rel, 'shows': shows, 'checks': []})
                pending = []
            elif word[0] == 'pixel' and cur:
                if pending:
                    cur['checks'].append(sentences(pending))
                pending = []
    return notes


def read_ppm(path):
    """(w, h, rgb) of a binary 8-bit PPM, or None if it is not one (yet)."""
    try:
        data = open(path, 'rb').read()
        m = re.match(rb'P6\s+(?:#[^\n]*\n\s*)*(\d+)\s+(\d+)\s+255\s', data)
        if not m:
            return None
        w, h = int(m.group(1)), int(m.group(2))
        rgb = data[m.end():m.end() + w * h * 3]
        return (w, h, rgb) if len(rgb) == w * h * 3 else None
    except OSError:
        return None


def png(w, h, rgb):
    def chunk(kind, body):
        return struct.pack('>I', len(body)) + kind + body + struct.pack('>I', zlib.crc32(kind + body) & 0xFFFFFFFF)
    stride = w * 3
    rows = b''.join(b'\x00' + rgb[y * stride:(y + 1) * stride] for y in range(h))
    return (b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', w, h, 8, 2, 0, 0, 0))
            + chunk(b'IDAT', zlib.compress(rows, 9)) + chunk(b'IEND', b''))


def half(w, h, rgb):
    """A 2x2 box average: the thumbnail."""
    stride, out = w * 3, bytearray()
    for y in range(0, h - 1, 2):
        a, b = rgb[y * stride:(y + 1) * stride], rgb[(y + 1) * stride:(y + 2) * stride]
        for x in range(0, stride - 3, 6):
            out += bytes(((a[x + c] + a[x + 3 + c] + b[x + c] + b[x + 3 + c] + 2) >> 2) for c in range(3))
    return w // 2, h // 2, bytes(out)


def write_if_changed(path, data):
    if os.path.exists(path) and open(path, 'rb').read() == data:
        return
    with open(path, 'wb') as f:
        f.write(data)


def convert(d):
    """Write the PNG and thumbnail for one dump; True if a PNG exists afterwards."""
    src = os.path.join(ROOT, 'build', d['dump'] + '.ppm')
    full = os.path.join(ROOT, OUT_DIR, d['name'] + '.png')
    for attempt in range(RETRIES):
        img = read_ppm(src) if os.path.exists(src) else None
        if img and img[:2] == (SCREEN_W, SCREEN_H):
            write_if_changed(full, png(*img))
            write_if_changed(os.path.join(ROOT, THUMB_DIR, d['name'] + '.png'), png(*half(*img)))
            return True
        if not os.path.exists(src):
            break
        if attempt + 1 < RETRIES:
            print(f'gallery: {src} is not a whole {SCREEN_W}x{SCREEN_H} dump yet; retrying in {RETRY_WAIT_S} s', file=sys.stderr)
            time.sleep(RETRY_WAIT_S)
    kept = os.path.exists(full) and os.path.exists(os.path.join(ROOT, THUMB_DIR, d['name'] + '.png'))
    print(f'gallery: no usable build/{d["dump"]}.ppm; {"kept the last PNG" if kept else "left out"}', file=sys.stderr)
    return kept


def introduced(dumps):
    """dump -> the first commit whose diff adds a line naming it under
    tests/smoke (one pass over the history, oldest first)."""
    found, commit = {}, None
    log = git('log', '--reverse', '--unified=0', '--format=%x01%h%x09%aI%x09%ct%x09%s', '--', 'tests/smoke')
    for line in log.splitlines():
        if line.startswith('\x01'):
            h, date, ts, subject = line[1:].split('\t', 3)
            commit = {'commit': h, 'date': date[:10], 'ts': int(ts), 'subject': subject}
        elif commit and line.startswith('+') and not line.startswith('+++'):
            for d in dumps:
                if d not in found and f'{d}.ppm' in line:
                    found[d] = commit
    return found


def wave_info(wave):
    """Wave file, title and slice headings (id -> (heading text, anchor))."""
    files = glob.glob(os.path.join(ROOT, WAVES_DIR, f'WAVE-{int(wave[1:]):02d}-*.md'))
    if not files:
        return None
    rel = os.path.relpath(files[0], ROOT).replace(os.sep, '/')
    text = open(files[0], encoding='utf-8').read()
    m = re.search(r'^# W\d+\s+(.+?)(?:\s+\(M\d+\))?(?:\s+—.*)?$', text, re.M)
    slices = {}
    for h in re.findall(r'^### (.+)$', text, re.M):
        h = re.sub(r'^\[[ xX~]\]\s*', '', h).strip()
        plain = re.sub(r'\[([^\]]*)\]\([^)]*\)', r'\1', h).replace('`', '').replace('**', '')
        sid = SLICE_RE.match(plain)
        if sid:
            slices[sid.group(1)] = (plain, slug(plain))
    return {'file': rel, 'title': m.group(1).strip() if m else wave, 'slices': slices}


def entry(d, notes, c):
    n = notes.get(d['name'], {'screen': None, 'shows': '', 'checks': []})
    e = {'name': d['name'], 'image': f'{OUT_DIR}/{d["name"]}.png', 'thumb': f'{THUMB_DIR}/{d["name"]}.png',
         'width': SCREEN_W, 'height': SCREEN_H,
         'caption': CAPTIONS.get(d['name']) or n['shows'] or (n['checks'][:1] or [d['name']])[0],
         'shows': n['shows'], 'checks': n['checks'], 'test': d['test'], 'keys': d['keys'], 'screen': n['screen'],
         'wave': d['header_wave'], 'wave_title': None, 'wave_file': None,
         'slice': None, 'slice_label': None, 'slice_title': None, 'slice_link': None,
         'commit': None, 'date': None, 'subject': None, 'ts': None}
    m = SLICE_RE.match(c['subject']) if c else None
    if c:
        e.update(commit=c['commit'], date=c['date'], subject=c['subject'], ts=c['ts'])
    if m:
        e['wave'] = f'W{m.group(2)}'
        e['slice'] = m.group(1)
        e['slice_label'] = m.group(1) + (f'..S{m.group(4)}' if m.group(4) else '')
    w = wave_info(e['wave']) if e['wave'] else None
    if w:
        e['wave_title'], e['wave_file'] = w['title'], w['file']
        if e['slice'] in w['slices']:
            e['slice_title'], anchor = w['slices'][e['slice']]
            e['slice_link'] = f'{w["file"]}#{anchor}'
    return e


def main():
    os.makedirs(os.path.join(ROOT, THUMB_DIR), exist_ok=True)
    dumps, notes = keys_dumps(), screen_notes()
    commits = introduced([d['dump'] for d in dumps])
    entries = []
    for i, d in enumerate(dumps):
        if not convert(d):
            continue
        e = entry(d, notes, commits.get(d['dump']))
        e['order'] = i
        entries.append(e)
    wave_no = lambda e: int(e['wave'][1:]) if e['wave'] else 1 << 30
    entries.sort(key=lambda e: (wave_no(e), e['ts'] if e['ts'] is not None else 1 << 62, e['order']))
    for e in entries:
        del e['ts'], e['order']
    keep = {os.path.basename(e['image']) for e in entries}
    for stale in glob.glob(os.path.join(ROOT, OUT_DIR, '*.png')) + glob.glob(os.path.join(ROOT, THUMB_DIR, '*.png')):
        if os.path.basename(stale) not in keep:
            os.remove(stale)
    write_if_changed(os.path.join(ROOT, MANIFEST), (json.dumps({'screenshots': entries}, indent=2, ensure_ascii=False) + '\n').encode())
    size = sum(os.path.getsize(p) for p in glob.glob(os.path.join(ROOT, OUT_DIR, '**', '*.png'), recursive=True))
    unmatched = [e['name'] for e in entries if not e['slice_link']]
    print(f'gallery: {len(entries)} screenshots, {size // 1024} KiB of PNG in {OUT_DIR}'
          + (f'; no slice link for {", ".join(unmatched)}' if unmatched else ''))


if __name__ == '__main__':
    main()
