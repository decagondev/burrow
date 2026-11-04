#!/usr/bin/env python3
"""net-budgets.py - check make net-bench's numbers (W17-S06) against the
network's budgets.

Usage: net-budgets.py <serial log> <fetch.py log>

Reads gofetch's "N bytes in M ms (X MB/s)" from Burrow's serial log and
fetch.py's "(Y connections a second)" from the host's, and fails (exit 1)
when one is under its budget or missing. The budgets sit below what Burrow
measured when they were set (QEMU TCG on the development machine): 10 MB
at 5.5 MB/s, 11.3 connections a second (httpd forks for each one).
Docs: docs/04-plan/waves/WAVE-17-networking.md (S06).
"""
import re
import sys

DOWNLOAD_MBS = 3     # least MB/s for 10 MB from the host with gofetch
CONNECTIONS_S = 6    # least short connections a second to busybox httpd

serial = open(sys.argv[1], errors='replace').read()
host = open(sys.argv[2], errors='replace').read()
m = re.search(r'gofetch: 200 OK, 10485760 bytes in \d+ ms \(([0-9.]+) MB/s\)', serial)
c = re.search(r'\(([0-9.]+) connections a second\)', host)
failed = False
for name, match, least, what in [("download", m, DOWNLOAD_MBS, "MB/s, 10 MB from the host with gofetch"),
                                 ("connections", c, CONNECTIONS_S, "connections a second to busybox httpd")]:
    value = float(match.group(1)) if match else None
    ok = value is not None and value >= least
    failed |= not ok
    shown = 'missing' if value is None else f'{value:g}'
    print(f"NET BENCH {'ok   ' if ok else 'UNDER'} {name} {shown} (budget at least {least:g} {what})")
sys.exit(1 if failed else 0)
