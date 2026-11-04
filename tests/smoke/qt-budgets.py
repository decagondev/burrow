#!/usr/bin/env python3
"""qt-budgets.py - check qtbench's numbers (W15-S05) against Qt's budgets.

Usage: qt-budgets.py <serial log of make qt-bench>

Reads the "qtbench: <name> <value> <unit>" lines and fails (exit 1) when
one is over its budget or missing. The budgets sit above what Burrow
measured when they were set (QEMU TCG on the development machine): first
frame 2.8 s, repaint 10.9 ms, 6.5 MiB, no idle wakes. Docs:
docs/04-plan/waves/WAVE-15-qt.md (S05).
"""
import re
import sys

BUDGETS = {                    # name: (most allowed, what it measures)
    'first-frame-ms': (2500, 'exec to the first full-screen frame'),  # 3500 until W18-S02's lazy execve
    'repaint-ms': (33, 'one full-screen repaint (30 frames a second)'),
    'memory-mib': (64, 'memory the system lost to the program'),
    'idle-wakes': (0, 'event-loop wakes in 2 s of idling'),
}

text = open(sys.argv[1], errors='replace').read()
seen = {m.group(1): float(m.group(2)) for m in re.finditer(r'qtbench: (\S+) (-?[0-9.]+)', text)}  # memory can read negative: the kernel's heap gives frames back meanwhile
failed = False
for name, (most, what) in BUDGETS.items():
    value = seen.get(name)
    ok = value is not None and value <= most
    failed |= not ok
    shown = 'missing' if value is None else f'{value:g}'
    print(f"QT BENCH {'ok  ' if ok else 'OVER'} {name} {shown} (budget {most:g}: {what})")
sys.exit(1 if failed else 0)
