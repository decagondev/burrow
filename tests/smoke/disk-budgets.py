#!/usr/bin/env python3
"""disk-budgets.py - check make disk-bench's numbers (W23-S04; AHCI and NVMe
since W27-S02): the same 64 MiB written and read through the IDE disk (hdb),
then the virtio one (vda), an AHCI one (sda) and an NVMe one (nvme0n1).

Usage: disk-budgets.py <serial log of the IDE boot> <serial log of another boot>...

Reads the kernel's "[diskbench] INFO: <disk>: write ... (N MiB/s), read in
... (M MiB/s)" line from each log, prints them, and fails (exit 1) when a
line is missing or a disk is slower than IDE at either (the budget W23's
plan set for virtio, kept for every newer interface: no slower).
Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md (S04), WAVE-27-real-pc.md (S02).
"""
import re
import sys

LINE = r'\[diskbench\] INFO: (\w+): write \d+ MiB in \S+ \(([\d.]+) MiB/s\), read in \S+ \(([\d.]+) MiB/s\)'

if len(sys.argv) < 3:
    sys.exit(__doc__)
speeds = []
for path in sys.argv[1:]:
    m = re.search(LINE, open(path, errors='replace').read())
    if not m:
        print(f'DISK BENCH MISS: no diskbench line in {path}')
        sys.exit(1)
    speeds.append((m.group(1), float(m.group(2)), float(m.group(3))))
(ide, ide_w, ide_r), others = speeds[0], speeds[1:]
failed = False
for disk, w, r in others:
    for what, a, b in (('write', ide_w, w), ('read', ide_r, r)):
        ok = b >= a
        failed |= not ok
        print(f"DISK BENCH {'ok  ' if ok else 'SLOW'} {what}: {ide} {a:.1f} MiB/s, {disk} {b:.1f} MiB/s "
              f'({b / max(a, 0.01):.1f}x; budget: no slower than IDE)')
sys.exit(1 if failed else 0)
