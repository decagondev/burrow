#!/usr/bin/env python3
"""browser-budgets.py - check NetSurf's first frame on Burrow (W24-S06)
against its budget.

Usage: browser-budgets.py <smoke-browser.log>

Reads "browser: first page laid out at (S)" that tests/smoke/keys/browser.keys
prints from NetSurf's -v log once it is closed: the seconds from NetSurf's
start to the test page's layout (html_box_convert_done; the first frame
follows it), and fails (exit 1) when it is over the budget or missing. The
budget sits above what Burrow measured when it was set (QEMU TCG on the
development machine, Xfbdev, the page from the host's http.server).
Docs: docs/04-plan/waves/WAVE-24-ssh-browser.md (S06).
"""
import re
import sys

FIRST_PAGE_S = 4.5  # most seconds from NetSurf's start to the page laid out (W25-S06: 2.71-2.82 measured; 6 in W24-S06)

log = open(sys.argv[1], errors='replace').read()
m = re.search(r'browser: first page laid out at \(([0-9.]+)\)', log)
value = float(m.group(1)) if m else None
ok = value is not None and value <= FIRST_PAGE_S
shown = 'missing' if value is None else f'{value:.2f} s'
print(f"BROWSER BENCH {'ok   ' if ok else 'MISS '} first page {shown} (budget at most {FIRST_PAGE_S:g} s)")
sys.exit(0 if ok else 1)
