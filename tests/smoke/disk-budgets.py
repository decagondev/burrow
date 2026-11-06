#!/usr/bin/env python3
"""disk-budgets.py - check make disk-bench's numbers (W23-S04): the same
64 MiB written and read through the IDE disk (hdb) and the virtio one (vda).

Usage: disk-budgets.py <serial log of the IDE boot> <serial log of the virtio boot>

Reads the kernel's "[diskbench] INFO: <disk>: write ... (N MiB/s), read in
... (M MiB/s)" line from each log, prints both, and fails (exit 1) when a
line is missing or virtio-blk is slower than IDE at either (the budget
W23's plan set: no slower).
Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md (S04).
"""
import re
import sys

LINE = r'\[diskbench\] INFO: (\w+): write \d+ MiB in \S+ \(([\d.]+) MiB/s\), read in \S+ \(([\d.]+) MiB/s\)'

speeds = []
for path in sys.argv[1:3]:
    m = re.search(LINE, open(path, errors='replace').read())
    if not m:
        print(f'DISK BENCH MISS: no diskbench line in {path}')
        sys.exit(1)
    speeds.append((m.group(1), float(m.group(2)), float(m.group(3))))
(ide, ide_w, ide_r), (vio, vio_w, vio_r) = speeds
for what, a, b in (('write', ide_w, vio_w), ('read', ide_r, vio_r)):
    ok = b >= a
    print(f"DISK BENCH {'ok  ' if ok else 'SLOW'} {what}: {ide} {a:.1f} MiB/s, {vio} {b:.1f} MiB/s "
          f'({b / max(a, 0.01):.1f}x; budget: virtio no slower)')
    if not ok:
        sys.exit(1)
