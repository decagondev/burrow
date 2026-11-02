#!/usr/bin/env python3
"""checkscreen.py - check pixels of QEMU screendumps against a spec.

Usage: checkscreen.py <spec-file>

Spec lines (a line starting with '#' is a comment; a colour is #rrggbb):
    size  <ppm> <width> <height>
    pixel <ppm> <x> <y> <colour>
The PPM paths are relative to the current directory (the repo root, where
smoke.sh runs). Exit 1 with one line per mismatch.
"""
import sys


def read_ppm(path):
    with open(path, 'rb') as f:
        data = f.read()
    fields, pos = [], 0
    while len(fields) < 4:                       # P6, width, height, maxval
        while data[pos:pos + 1].isspace():
            pos += 1
        if data[pos:pos + 1] == b'#':
            pos = data.index(b'\n', pos) + 1
            continue
        end = pos
        while not data[end:end + 1].isspace():
            end += 1
        fields.append(data[pos:end])
        pos = end
    if fields[0] != b'P6' or fields[3] != b'255':
        raise SystemExit(f'checkscreen: {path} is not an 8-bit P6 PPM')
    w, h = int(fields[1]), int(fields[2])
    return w, h, data[pos + 1:]


def main():
    images, failures, checks = {}, [], 0
    with open(sys.argv[1]) as spec:
        for n, line in enumerate(spec, 1):
            if not line.strip() or line.lstrip().startswith('#'):
                continue                         # comments are whole lines: colours use '#'
            words = line.split()
            op, path = words[0], words[1]
            if path not in images:
                images[path] = read_ppm(path)
            w, h, rgb = images[path]
            checks += 1
            if op == 'size':
                if (w, h) != (int(words[2]), int(words[3])):
                    failures.append(f'{path}: size {w}x{h}, want {words[2]}x{words[3]}')
            elif op == 'pixel':
                x, y, want = int(words[2]), int(words[3]), words[4].lower()
                i = (y * w + x) * 3
                got = '#%02x%02x%02x' % tuple(rgb[i:i + 3])
                if got != want:
                    failures.append(f'{path}: pixel ({x},{y}) is {got}, want {want} (line {n})')
            else:
                raise SystemExit(f'checkscreen: unknown op {op!r} on line {n}')
    for f in failures:
        print('  ' + f)
    if failures:
        sys.exit(1)
    print(f'SCREEN PASS ({checks} checks)')


if __name__ == '__main__':
    main()
