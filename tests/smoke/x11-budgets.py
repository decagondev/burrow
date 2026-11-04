#!/usr/bin/env python3
"""x11-budgets.py - check xbench's numbers (W16-S05) against the desktop's budgets.

Usage: x11-budgets.py <serial log of make x11-bench>

Reads the "xbench: <name> <value>" lines and fails (exit 1) when one is
over its budget or missing. Times have a most allowed; Doom in X has a
least allowed share of the fbdev Doom's frame rate, measured in the same
boot, so a slower or faster host moves both. The budgets sit above what
Burrow measured when they were set (QEMU TCG on the development machine):
server 0.85 s, st 1.9 s, st with 100 lines 2.0 s, Doom 39.4 fps in X and
124.4 on fbdev (0.32: a 1 MB XPutImage a frame through the socket, as
TinyX has no MIT-SHM; still above Doom's 35 tics a second). Since W18-S04
Doom draws through MIT-SHM: 95 fps in X against 125 on fbdev, 0.76.
Docs: docs/04-plan/waves/WAVE-16-x11-desktop.md (S05).
"""
import re
import sys

MOST = {                       # name: (most allowed, what it measures)
    'server-ms': (1500, "Xfbdev's exec to a client's first frame"),
    'st-ms': (3000, "st's exec to its window mapped"),
    'st-ls-ms': (3500, "st's exec, 100 lines scrolled, st gone"),
}
X11_SHARE = 0.6                # least doom-x11-fps / doom-fbdev-fps

text = open(sys.argv[1], errors='replace').read()
seen = {m.group(1): float(m.group(2)) for m in re.finditer(r'xbench: (\S+) ([0-9.]+)', text)}
failed = False
for name, (most, what) in MOST.items():
    value = seen.get(name)
    ok = value is not None and value <= most
    failed |= not ok
    shown = 'missing' if value is None else f'{value:g}'
    print(f"X11 BENCH {'ok  ' if ok else 'OVER'} {name} {shown} (budget {most:g}: {what})")
fb, x = seen.get('doom-fbdev-fps'), seen.get('doom-x11-fps')
share = x / fb if fb and x is not None else None
ok = share is not None and share >= X11_SHARE
failed |= not ok
shown = 'missing' if share is None else f'{x:g} fps in X, {fb:g} on fbdev: {share:.2f}'
print(f"X11 BENCH {'ok  ' if ok else 'UNDER'} doom-x11-share {shown} (budget at least {X11_SHARE:g})")
sys.exit(1 if failed else 0)
