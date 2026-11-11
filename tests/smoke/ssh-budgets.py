#!/usr/bin/env python3
"""ssh-budgets.py - check make ssh-bench's numbers (W24-S06) against the
budgets for ssh.

Usage: ssh-budgets.py <ssh-host.log>

Reads tests/smoke/ssh.py's "SSH BENCH login X s ... scp 4 MiB up Y MB/s ...
down Z MB/s" and fails (exit 1) when one is over or under its budget or
missing. The budgets sit below what Burrow measured when they were set
(QEMU TCG on the development machine): a key login in 1.2-1.7 s, scp at
1.0-1.2 MB/s each way. scp's limit is not the cipher (sha256 hashes 14 MB/s
on Burrow; chacha20-poly1305 and aes128-ctr stream alike) nor dropbear's
window (a 1 MiB one gains 15%): it is the cost of dropbear's blocking calls
in Burrow (RISKS.md). The per-cipher stream lines are printed, not budgeted.
Docs: docs/04-plan/waves/WAVE-24-ssh-browser.md (S06).
"""
import re
import sys

LOGIN_S = 3.0   # most seconds for a key login running true
SCP_MBS = 0.8   # least MB/s for 4 MiB scp'd either way

log = open(sys.argv[1], errors='replace').read()
m = re.search(r'SSH BENCH login ([0-9.]+) s .* up ([0-9.]+) MB/s \(status 0\), down ([0-9.]+) MB/s \(status 0\)', log)
values = [float(v) for v in m.groups()] if m else [None, None, None]
failed = False
for name, value, ok_if, what in [("login", values[0], lambda v: v <= LOGIN_S, f"at most {LOGIN_S:g} s"),
                                 ("scp up", values[1], lambda v: v >= SCP_MBS, f"at least {SCP_MBS:g} MB/s"),
                                 ("scp down", values[2], lambda v: v >= SCP_MBS, f"at least {SCP_MBS:g} MB/s")]:
    ok = value is not None and ok_if(value)
    failed |= not ok
    shown = 'missing' if value is None else f'{value:g}'
    print(f"SSH BENCH {'ok   ' if ok else 'MISS '} {name} {shown} (budget {what})")
sys.exit(1 if failed else 0)
