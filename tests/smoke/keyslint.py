#!/usr/bin/env python3
"""keyslint.py - BUG-0033's lesson, checked (W25-S04): a keys file's `wait`
must not be satisfied by the echo of the line typed just before it. The
console echoes every typed line, wrapped at 80 columns, so a `wait demo: up`
after typing `echo demo: up''` matched the echo when the wrap did not split
the marker, and the step after it ran 25 seconds early. Split a marker in
the middle (`demo: u''p`) so the echo never contains it. A line typed into
an X window (st) echoes there, not on the console: a comment containing
"keyslint: typed into X" before it says so. sendkeys.py runs this on its
keys file before it starts; `make test` runs it on all of them. Never
changes a file.

Usage: keyslint.py <keys-file>...   (exit 1 and the offending lines if any)
"""
import sys


EXEMPT = 'keyslint: typed into X'   # a comment: the next typed line goes to an X window, not the console


def problems(lines):
    """(line number, wait text, typed text) for each wait its last typed line contains."""
    out, typed, typed_no, exempt = [], None, 0, False
    for no, raw in enumerate(lines, 1):
        line = raw.rstrip('\n')
        if line.startswith('#') and EXEMPT in line:
            exempt = True
        elif line.startswith('type '):
            typed, typed_no = (None if exempt else line[5:]), no
            exempt = False
        elif line.startswith('wait ') and typed is not None:
            want = line[5:]
            if want.strip() and want in typed:
                out.append((no, want, typed_no))
            typed = None   # a wait consumes the echo: later waits look at later output
    return out


def main():
    bad = 0
    for path in sys.argv[1:]:
        with open(path) as f:
            for no, want, typed_no in problems(f.readlines()):
                print("%s:%d: wait %r is in the line typed at line %d: its echo can satisfy it (split the marker: BUG-0033)"
                      % (path, no, want, typed_no))
                bad += 1
    sys.exit(1 if bad else 0)


if __name__ == '__main__':
    main()
