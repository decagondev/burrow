#!/usr/bin/env python3
"""ppm2png.py - convert a QEMU screendump (binary PPM) to PNG, stdlib only.

Usage: scripts/ppm2png.py <in.ppm> <out.png>
"""
import struct
import sys
import zlib

sys.path.insert(0, __import__('os').path.join(__import__('os').path.dirname(__file__), '..', 'tests', 'smoke'))
from checkscreen import read_ppm  # noqa: E402


def chunk(kind, data):
    body = kind + data
    return struct.pack('>I', len(data)) + body + struct.pack('>I', zlib.crc32(body) & 0xFFFFFFFF)


def main():
    w, h, rgb = read_ppm(sys.argv[1])
    rows = b''.join(b'\x00' + rgb[y * w * 3:(y + 1) * w * 3] for y in range(h))
    png = (b'\x89PNG\r\n\x1a\n'
           + chunk(b'IHDR', struct.pack('>IIBBBBB', w, h, 8, 2, 0, 0, 0))
           + chunk(b'IDAT', zlib.compress(rows, 9))
           + chunk(b'IEND', b''))
    with open(sys.argv[2], 'wb') as f:
        f.write(png)


if __name__ == '__main__':
    main()
