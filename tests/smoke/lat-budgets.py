#!/usr/bin/env python3
"""lat-budgets.py - check make lat-bench's round trips (W25) against their
budgets.

Usage: lat-budgets.py <smoke-lat-bench.log>

Reads user/lat's "lat: <name> <us> us" lines from Burrow's serial log and
fails (exit 1) when one is over its budget or missing; the nucleus's steps
of getpid ("lat: step ...") are printed with them, not budgeted. W25-S01
set the budgets above what Burrow measured at the start of the wave (QEMU
TCG on the development machine, four CPUs, seven boots): getpid 101-155
us, a clock read 110-164, pipe 351-437, futex 116-211, socketpair 379-602,
loopback TCP 1933-2713. TCG varies by half from boot to boot with the
host's load (PIT-89), so the budgets leave room. Its steps for getpid, on
the quiet boots: gate to taken 14.5, taken to replied (Go) 68-70, replied
to resumed 16-17, out 1.4. The wave's later slices only tighten them; M25
wants each ping-pong and getpid at least twice as fast as the quiet boots.
Docs: docs/04-plan/waves/WAVE-25-speed.md.
"""
import re
import sys

BUDGETS_US = {          # most microseconds per operation (W25-S01's baseline)
    "getpid": 200,
    "clock": 2,       # W25-S02: the vDSO (0.08 us measured; 110-164 before)
    "pipe": 600,
    "futex": 280,
    "socketpair": 900,
    "tcp": 3500,
}

log = open(sys.argv[1], errors='replace').read()
failed = False
for name, most in BUDGETS_US.items():
    m = re.search(r'^lat: %s ([0-9.]+) us' % name, log, re.M)
    value = float(m.group(1)) if m else None
    ok = value is not None and value <= most
    failed |= not ok
    shown = 'missing' if value is None else '%.2f us' % value
    print("LAT BENCH %s %s %s (budget at most %g us)" % ('ok   ' if ok else 'MISS ', name, shown, most))
for step, us in re.findall(r'^lat: step (\S+) ([0-9.]+) us', log, re.M):
    print("LAT BENCH step  getpid %s %s us" % (step, us))
sys.exit(1 if failed else 0)
