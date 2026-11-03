#!/usr/bin/env python3
"""sendkeys.py - type into a QEMU guest's PS/2 keyboard through the monitor.

Usage: sendkeys.py <monitor-socket> <serial-log> <keys-file> [timeout-seconds]

The keys file is a small script, one step per line ('#' comments):
    wait <text>    block until <text> appears in the serial log after
                   what the previous wait matched
    type <text>    press each character (letters, digits, space, - . / , = ;)
    key <name>     press one QEMU key name (ret, backspace, ctrl-u, ...)
    screendump <f> save the screen as a PPM (checked by checkscreen.py)
smoke.sh runs this in the background next to QEMU; a step that times out
exits 1 and smoke.sh then fails on its missing markers.
"""
import os
import socket
import sys
import time

KEY_DELAY_S = 0.02            # between keys: the guest drains the 8042 per IRQ
NAMES = {' ': 'spc', '-': 'minus', '.': 'dot', '/': 'slash', ',': 'comma',
         '=': 'equal', ';': 'semicolon', "'": 'apostrophe',
         '|': 'shift-backslash', '>': 'shift-dot', '<': 'shift-comma', '$': 'shift-4',
         '?': 'shift-slash', '"': 'shift-apostrophe', '_': 'shift-minus', '&': 'shift-7',
         '*': 'shift-8', '!': 'shift-1', ':': 'shift-semicolon',
         '(': 'shift-9', ')': 'shift-0', '\\': 'backslash'}


def key_name(ch):
    if ch.isascii() and (ch.islower() or ch.isdigit()):
        return ch
    if ch.isascii() and ch.isupper():
        return 'shift-' + ch.lower()
    if ch in NAMES:
        return NAMES[ch]
    raise SystemExit(f'sendkeys: no key for {ch!r}')


def connect(path, deadline):
    while time.time() < deadline:
        try:
            s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            s.connect(path)
            return s
        except OSError:
            time.sleep(0.05)
    raise SystemExit(f'sendkeys: no monitor at {path}')


def wait_for(log, text, deadline, cursor):
    """Returns the log offset just past text, searching from cursor on, so
    a wait never matches output that an earlier wait already passed."""
    while time.time() < deadline:
        if os.path.exists(log):
            with open(log, 'rb') as f:
                i = f.read().find(text.encode(), cursor)
            if i >= 0:
                return i + len(text)
        time.sleep(0.05)
    raise SystemExit(f'sendkeys: {text!r} never appeared in {log} after offset {cursor}')


def wait_for_file(path, deadline):
    """QEMU writes the dump asynchronously: wait until its size settles."""
    last = -1
    while time.time() < deadline:
        size = os.path.getsize(path) if os.path.exists(path) else -1
        if size > 0 and size == last:
            return
        last = size
        time.sleep(0.1)
    raise SystemExit(f'sendkeys: QEMU never wrote {path}')


def main():
    mon, log, keys = sys.argv[1:4]
    deadline = time.time() + float(sys.argv[4] if len(sys.argv) > 4 else 30)
    s = connect(mon, deadline)

    def press(name):
        s.sendall(f'sendkey {name}\n'.encode())
        time.sleep(KEY_DELAY_S)

    cursor = 0
    with open(keys) as f:
        for raw in f:
            line = raw.rstrip('\n')
            if not line.strip() or line.lstrip().startswith('#'):
                continue
            op, _, arg = line.partition(' ')
            if op == 'wait':
                cursor = wait_for(log, arg, deadline, cursor)
            elif op == 'type':
                for ch in arg:
                    press(key_name(ch))
            elif op == 'key':
                press(arg)
            elif op == 'screendump':
                path = os.path.abspath(arg)
                if os.path.exists(path):
                    os.remove(path)
                s.sendall(f'screendump {path}\n'.encode())
                wait_for_file(path, deadline)
            else:
                raise SystemExit(f'sendkeys: unknown step {line!r}')
    s.close()


if __name__ == '__main__':
    main()
