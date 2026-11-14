#!/usr/bin/env python3
"""virtio-expect.py - an expect file for the same smoke on other disks
(W23-S04, make virtio-smoke; W27-S02, make storage-smoke): the data disks
that were IDE's hdb, hdc and hdd are virtio's vda, vdb and vdc, AHCI's sda,
sdb and sdc, or NVMe's nvme0n1, nvme1n1 and nvme2n1, in the order QEMU's
command line gives them. The disk driver's lines ("[virtioblk] INFO: vdY ",
"[ahci] INFO: sdY: ", "[nvme] INFO: nvmeYn1: "), one per disk the file
names, stand where its first disk line was (an "[ata] INFO: hdX: ..." probe
line is dropped: the driver's took its place). The IDE expect files stay
the one source; this writes a derived copy into build/, never into tests/.

Usage: virtio-expect.py [--if virtio|ahci|nvme] <ide-expect.txt> <out.txt>
"""
import re
import sys

NAMES = {
    "virtio": {"hdb": "vda", "hdc": "vdb", "hdd": "vdc"},
    "ahci": {"hdb": "sda", "hdc": "sdb", "hdd": "sdc"},
    "nvme": {"hdb": "nvme0n1", "hdc": "nvme1n1", "hdd": "nvme2n1"},
}
DRIVER_LINE = {
    "virtio": "[virtioblk] INFO: %s \n",
    "ahci": "[ahci] INFO: %s: \n",
    "nvme": "[nvme] INFO: %s: \n",
}


def convert(lines, iface="virtio"):
    """The lines with disks renamed; the driver's lines (it starts every disk
    before any is used) go where the first disk line was."""
    names = NAMES[iface]
    found = []
    for line in lines:
        for hd, new in names.items():
            if ("INFO: %s: " % hd) in line and new not in found:
                found.append(new)
    found.sort(key=list(names.values()).index)
    out, placed = [], False
    for line in lines:
        disk = any(("INFO: %s: " % hd) in line for hd in names)
        if disk and not placed:
            out.extend(DRIVER_LINE[iface] % new for new in found)
            placed = True
        if re.match(r"\[ata\] INFO: hd[bcd]: ", line):
            continue  # the probe line: the driver's took its place
        for hd, new in names.items():
            line = line.replace("INFO: %s: " % hd, "INFO: %s: " % new)
        out.append(line)
    return out


def main():
    args = sys.argv[1:]
    iface = "virtio"
    if len(args) == 4 and args[0] == "--if" and args[1] in NAMES:
        iface, args = args[1], args[2:]
    if len(args) != 2:
        sys.exit(__doc__)
    with open(args[0]) as f:
        lines = f.readlines()
    with open(args[1], "w") as f:
        f.write("# Derived from %s by tests/smoke/virtio-expect.py --if %s: do not edit.\n" % (args[0], iface))
        f.writelines(convert(lines, iface))


if __name__ == "__main__":
    main()
