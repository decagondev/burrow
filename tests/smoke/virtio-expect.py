#!/usr/bin/env python3
"""virtio-expect.py - an expect file for the same smoke on virtio disks
(W23-S04, make virtio-smoke): the data disks that were IDE's hdb, hdc and
hdd are virtio's vda, vdb and vdc, in the order QEMU's command line gives
them. virtioblk's "[virtioblk] INFO: vdY " lines, one per disk the file
names, stand where its first disk line was (an "[ata] INFO: hdX: ..." probe
line is dropped: virtioblk's took its place). The IDE expect files stay
the one source; this writes a derived copy into build/, never into tests/.

Usage: virtio-expect.py <ide-expect.txt> <out.txt>
"""
import re
import sys

NAMES = {"hdb": "vda", "hdc": "vdb", "hdd": "vdc"}


def convert(lines):
    """The lines with disks renamed; virtioblk's lines (it starts every disk
    before any is used) go where the first disk line was."""
    found = []
    for line in lines:
        for hd, vd in NAMES.items():
            if ("INFO: %s: " % hd) in line and vd not in found:
                found.append(vd)
    out, placed = [], False
    for line in lines:
        disk = any(("INFO: %s: " % hd) in line for hd in NAMES)
        if disk and not placed:
            out.extend("[virtioblk] INFO: %s \n" % vd for vd in sorted(found))
            placed = True
        if re.match(r"\[ata\] INFO: hd[bcd]: ", line):
            continue  # the probe line: virtioblk's took its place
        for hd, vd in NAMES.items():
            line = line.replace("INFO: %s: " % hd, "INFO: %s: " % vd)
        out.append(line)
    return out


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    with open(sys.argv[1]) as f:
        lines = f.readlines()
    with open(sys.argv[2], "w") as f:
        f.write("# Derived from %s by tests/smoke/virtio-expect.py: do not edit.\n" % sys.argv[1])
        f.writelines(convert(lines))


if __name__ == "__main__":
    main()
