#!/usr/bin/env python3
"""browser-pages.py - the browser's test pages (W24-S04): writes into a
directory an index page with headings, a styled box, a PNG and a link, the
page the link leads to, and the PNG itself (drawn here, so that no binary
lives in the repository). The colours are the ones the smokes look for on
the screen: the box is BOX_RGB, the image is IMAGE_RGB, the second page's
heading sits on PAGE2_RGB. Never fetches anything.

Usage: browser-pages.py <dir>
"""
import os
import struct
import sys
import zlib

BOX_RGB = (0x20, 0xa0, 0x40)     # the styled box: a solid green
IMAGE_RGB = (0xd0, 0x30, 0x30)   # the PNG: a solid red square
PAGE2_RGB = (0x30, 0x50, 0xc0)   # the second page's banner: blue
IMAGE_PX = 64

INDEX = """<!DOCTYPE html>
<html><head><title>Burrow test page</title>
<style>
body { background: #ffffff; color: #000000; margin: 16px; font-family: sans-serif; }
h1 { font-size: 24px; }
.box { background: #%02x%02x%02x; color: #ffffff; width: 240px; height: 80px; padding: 8px; }
</style></head>
<body>
<h1>Burrow test page</h1>
<h2>Served by the host</h2>
<div class="box">A styled box</div>
<p><img src="square.png" width="%d" height="%d" alt="a red square"></p>
<p><a href="second.html">The second page</a></p>
</body></html>
""" % (BOX_RGB + (IMAGE_PX, IMAGE_PX))

SECOND = """<!DOCTYPE html>
<html><head><title>The second page</title>
<style>
body { background: #ffffff; margin: 16px; font-family: sans-serif; }
.banner { background: #%02x%02x%02x; color: #ffffff; height: 120px; padding: 8px; }
</style></head>
<body><div class="banner"><h1>The second page</h1></div>
<p><a href="index.html">Back</a></p></body></html>
""" % PAGE2_RGB


def png(width, height, rgb):
    """A truecolour PNG of one colour."""
    row = b"\x00" + bytes(rgb) * width
    raw = row * height

    def chunk(kind, data):
        body = kind + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)
    ihdr = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")


def main():
    d = sys.argv[1]
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, "index.html"), "w") as f:
        f.write(INDEX)
    with open(os.path.join(d, "second.html"), "w") as f:
        f.write(SECOND)
    with open(os.path.join(d, "square.png"), "wb") as f:
        f.write(png(IMAGE_PX, IMAGE_PX, IMAGE_RGB))


if __name__ == "__main__":
    main()
