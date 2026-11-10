#!/usr/bin/env python3
"""xwdpixel.py - pixel checks on an X screen dump (W24-S04): reads an XWD
file (xwd -root, 24- or 32-bit ZPixmap, as Xvfb gives) and checks that each
x y rrggbb triple names the colour at that point, within a small tolerance.
Exits 1 and says which point differs. Never changes the file.

Usage: xwdpixel.py <file.xwd> x y rrggbb [x y rrggbb ...]
"""
import struct
import sys

TOLERANCE = 24  # per channel: a scaled image or a dithered edge


def pixel(data, x, y):
    h = struct.unpack(">25I", data[:100])
    header_bytes, width, height, bytes_per_line, ncolors = h[0], h[4], h[5], h[12], h[19]
    if not (0 <= x < width and 0 <= y < height):
        raise SystemExit("xwdpixel: (%d,%d) is outside %dx%d" % (x, y, width, height))
    off = header_bytes + ncolors * 12 + y * bytes_per_line + x * 4
    b, g, r = data[off], data[off + 1], data[off + 2]
    return r, g, b


def main():
    args = sys.argv[1:]
    with open(args[0], "rb") as f:
        data = f.read()
    bad = 0
    for i in range(1, len(args), 3):
        x, y, want = int(args[i]), int(args[i + 1]), args[i + 2]
        exp = tuple(int(want[j:j + 2], 16) for j in (0, 2, 4))
        got = pixel(data, x, y)
        if any(abs(a - b) > TOLERANCE for a, b in zip(got, exp)):
            print("xwdpixel: (%d,%d) is %02x%02x%02x, want %s" % (x, y, got[0], got[1], got[2], want))
            bad += 1
    sys.exit(1 if bad else 0)


if __name__ == "__main__":
    main()
