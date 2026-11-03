#!/usr/bin/env python3
"""checkdisk.py - did the kernel's disk writes reach the disk? (W13-S03)

Usage: checkdisk.py <image> <sectors>

After `make disk-smoke`, the last <sectors> sectors of the scratch image must
hold the pattern kernel/cmd/kernel/disktest.go writes: each sector starts
with "burrow disktest lba N\\n" and continues with byte(i*13 + N). Exits 1
on the first sector that does not.
"""
import sys

SECTOR = 512


def expected(lba):
    b = bytearray((i * 13 + lba) & 0xFF for i in range(SECTOR))
    head = f"burrow disktest lba {lba}\n".encode()
    b[:len(head)] = head
    return bytes(b)


def main():
    path, count = sys.argv[1], int(sys.argv[2])
    data = open(path, 'rb').read()
    total = len(data) // SECTOR
    for lba in range(total - count, total):
        if data[lba * SECTOR:(lba + 1) * SECTOR] != expected(lba):
            print(f"DISK WRITE FAIL: sector {lba} of {path} does not hold the pattern")
            sys.exit(1)
    if any(data[:(total - count) * SECTOR]):
        print(f"DISK WRITE FAIL: bytes before sector {total - count} changed")
        sys.exit(1)
    print(f"DISK WRITE PASS ({count} sectors at the end of {path})")


if __name__ == '__main__':
    main()
