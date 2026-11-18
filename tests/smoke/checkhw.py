#!/usr/bin/env python3
"""checkhw.py - the hardware report in a smoke log names a driver for every
PCI function a desktop needs (W28-S05, make q35-full-smoke).

Usage: checkhw.py <serial-log>

Reads the first hardware report in the log (`hw` on the console, or kargs
hwreport: from its "pci:" line to its "usb:" line) and fails when a function
other than a bridge (class 06) or an SMBus controller (class 0c.05) is
listed as "no driver". Prints the functions it saw and what drives each.
"""
import re
import sys

ALLOWED = (re.compile(r'\(class 06\.'), re.compile(r'\(class 0c\.05\.'))


def report(lines):
    """The PCI lines of the first report: between "pci:" and "usb:"."""
    out, inside = [], False
    for line in lines:
        s = line.strip()
        if s == 'pci:':
            inside, out = True, []
            continue
        if inside and s == 'usb:':
            return out
        if inside and s:
            out.append(s)
    return out if inside else None


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__.strip().splitlines()[2])
    with open(sys.argv[1], encoding='utf-8', errors='replace') as f:
        lines = f.read().splitlines()
    pci = report(lines)
    if not pci:
        sys.exit(f'HW REPORT FAIL: no report (pci: ... usb:) in {sys.argv[1]}')
    missing = [l for l in pci if l.endswith(': no driver') and not any(a.search(l) for a in ALLOWED)]
    for l in pci:
        print(f'checkhw: {l}')
    if missing:
        sys.exit('HW REPORT FAIL: no driver for ' + '; '.join(missing))
    driven = sum(1 for l in pci if not l.endswith(': no driver'))
    print(f'HW REPORT PASS ({driven} of {len(pci)} PCI functions driven; the rest are bridges and the SMBus)')


if __name__ == '__main__':
    main()
