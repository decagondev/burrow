#!/usr/bin/env python3
"""mem-budgets.py - check make mem-bench's numbers (W18-S06) against the
memory budgets.

Usage: mem-budgets.py <serial log of make mem-bench>

Reads forkbench's lines and fails (exit 1) when one is over its budget or
missing. The budgets sit above what Burrow measured when they were set
(QEMU TCG on the development machine): a small fork 2.1-2.5 ms, a 64 MiB
fork 48-52 ms (324 ms with W10's eager copy, 86 ms with W18-S01's
copy-on-write before S06 batched it), a waiting child within the noise of
the kernel's own heap (58 MB with the eager copy). The host's Linux, for
scale: 0.22 ms and 3.7 ms. Qt's start-up and Doom's MIT-SHM share have
their budgets in qt-budgets.py and x11-budgets.py.
Docs: docs/04-plan/waves/WAVE-18-foundations.md (S06).
"""
import re
import sys

BUDGETS = [  # (pattern, most allowed, unit, what it measures)
    (r'fork of a small process: (\d+) us', 5000, 'us', 'fork, exit and waitpid of a small process'),
    (r'fork of a 64 MiB process: (\d+) us', 100000, 'us', 'the same with 64 MiB touched (copy-on-write)'),
    (r'a waiting child costs (-?\d+) KiB', 4096, 'KiB', 'memory a forked child costs before it writes'),
]

text = open(sys.argv[1], errors='replace').read()
failed = False
for pattern, most, unit, what in BUDGETS:
    m = re.search(r'forkbench: ' + pattern, text)
    value = int(m.group(1)) if m else None
    ok = value is not None and value <= most
    failed |= not ok
    shown = 'missing' if value is None else f'{value} {unit}'
    print(f"MEM BENCH {'ok  ' if ok else 'OVER'} {shown} (budget {most} {unit}: {what})")
sys.exit(1 if failed else 0)
