#!/usr/bin/env python3
"""smp-budgets.py - check make smp-bench's numbers (W20-S08): the same
CPU-bound programs on one CPU and on four.

Usage: smp-budgets.py <serial log on 1 CPU> <serial log on 4 CPUs>

Reads cpubench's and gocpubench's lines from both logs, prints the times
and speed-ups, and fails (exit 1) when a line is missing, a run reports the
wrong number of CPUs, or four CPUs are less than MIN_SPEEDUP times faster
(M20 asks 2.5 for the C program; the Go one has the same budget).
Docs: docs/04-plan/waves/WAVE-20-smp.md (S08).
"""
import re
import sys

MIN_SPEEDUP = 2.5
PROGRAMS = [  # (name, pattern: ms, cpus)
    ('cpubench', r'cpubench: \d+ threads x \d+ rounds in (\d+) ms on (\d+) CPUs'),
    ('gocpubench', r'gocpubench: \d+ goroutines x \d+ rounds in (\d+) ms with GOMAXPROCS (\d+)'),
]

logs = [open(p, errors='replace').read() for p in sys.argv[1:3]]
failed = False
for name, pattern in PROGRAMS:
    found = [re.search(pattern, text) for text in logs]
    if not all(found):
        print(f'SMP BENCH MISS {name}: no result on {"1" if not found[0] else "4"} CPU(s)')
        failed = True
        continue
    (ms1, cpus1), (ms4, cpus4) = [(int(m.group(1)), int(m.group(2))) for m in found]
    if (cpus1, cpus4) != (1, 4):
        print(f'SMP BENCH CPUS {name}: saw {cpus1} and {cpus4}, want 1 and 4')
        failed = True
        continue
    speedup = ms1 / max(ms4, 1)
    ok = speedup >= MIN_SPEEDUP
    failed |= not ok
    print(f"SMP BENCH {'ok  ' if ok else 'SLOW'} {name}: {ms1} ms on 1 CPU, {ms4} ms on 4: "
          f'{speedup:.2f}x (budget {MIN_SPEEDUP}x)')
sys.exit(1 if failed else 0)
