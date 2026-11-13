#!/usr/bin/env python3
"""boot-budgets.py - check make boot-bench's boot times (W26-S06) against
their budgets.

Usage: boot-budgets.py <boot-bench.txt> <runs>

Reads tests/smoke/bootbench.py's "boot: <firmware> run <n> first <ms> ..."
lines, wants <runs> of each firmware, and prints each firmware's median
time for the boot's parts: the firmware after its first line on COM1 (to
Burrow's first line: OVMF's boot manager; nothing under SeaBIOS, which
writes nothing to COM1), Burrow's front half (stage1, stage2 and the
loader, or the UEFI loader, to the nucleus's first line), the nucleus (to
BURROW:NUCLEUS:OK), and the Go kernel's start (to BURROW:GO:OK). It fails
(exit 1) when a run is missing or a median of the whole, first line to
BURROW:GO:OK, is over its budget. Medians, because TCG's speed moves with
the host's load (PIT-89).

W26-S06 set the budgets above what it measured (QEMU TCG on the
development machine, four CPUs, three boots each on 2026-11-13): BIOS
15.40-15.49 s, of which 14.6 s is the loader reading the 7.4 MB kernel and
the 18 MB initrd by PIO (about 1.75 MB/s); UEFI 7.28-7.34 s: OVMF's boot
manager 1.9 s, the UEFI loader 3.4 s (the payloads through the firmware's
block I/O, about 8 MB/s), the nucleus 0.56 s and 1.44 s to the Go kernel,
1.1 s more than the BIOS path takes for it (BUG-0036). On one CPU: BIOS
15.5 s, UEFI 6.9 s. Before its first line OVMF spends another 2.2 s
(SeaBIOS 0.15 s), which no budget counts.
Docs: docs/04-plan/waves/WAVE-26-uefi.md (S06).
"""
import re
import statistics
import sys

BUDGETS_MS = {          # most milliseconds from the first line to BURROW:GO:OK (median)
    "bios": 20000,      # 15400-15490 measured (W26-S06)
    "uefi": 10000,      # 7280-7340 measured (W26-S06)
}
PARTS = (               # (what, from mark, to mark)
    ("firmware", "first", "burrow"),
    ("front half", "burrow", "nucleus"),
    ("nucleus", "nucleus", "nucleus-ok"),
    ("go start", "nucleus-ok", "go"),
    ("total", "first", "go"),
)
LINE = re.compile(r"^boot: (\w+) run \d+ ((?:[\w-]+ \d+ ?)+)$")

runs = {}
for line in open(sys.argv[1], errors="replace"):
    m = LINE.match(line.strip())
    if m:
        f = m.group(2).split()
        runs.setdefault(m.group(1), []).append({f[i]: int(f[i + 1]) for i in range(0, len(f), 2)})

want = int(sys.argv[2])
failed = False
for fw, budget in BUDGETS_MS.items():
    got = runs.get(fw, [])
    if len(got) != want:
        print(f"BOOT BENCH MISS: {len(got)} {fw} boots, want {want}")
        failed = True
        continue
    medians = {what: statistics.median(r[b] - r[a] for r in got) for what, a, b in PARTS}
    spread = [r["go"] - r["first"] for r in got]
    print(f"boot {fw}: " + ", ".join(f"{what} {ms:.0f} ms" for what, ms in medians.items())
          + f" (totals {min(spread)}-{max(spread)} ms)")
    if medians["total"] > budget:
        print(f"BOOT BENCH SLOW {fw}: {medians['total']:.0f} ms, budget {budget} ms")
        failed = True
    else:
        print(f"BOOT BENCH ok   {fw}: {medians['total']:.0f} ms, budget {budget} ms")
sys.exit(1 if failed else 0)
