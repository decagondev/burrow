#!/usr/bin/env python3
"""sendkeys.py - type into a QEMU guest's PS/2 keyboard through the monitor.

Usage: sendkeys.py <monitor-socket> <serial-log> <keys-file> [timeout-seconds]

The keys file is a small script, one step per line ('#' comments):
    wait <text>    block until <text> appears in the serial log
    type <text>    press each character (letters, digits, space, - . / , = ;)
    key <name>     press one QEMU key name (ret, backspace, ctrl-u, ...)
smoke.sh runs this in the background next to QEMU; a step that times out
exits 1 and smoke.sh then fails on its missing markers.
"""
import os
import socket
import sys
import time

KEY_DELAY_S = 0.02            # between keys: the guest drains the 8042 per IRQ
NAMES = {' ': 'spc', '-': 'minus', '.': 'dot', '/': 'slash', ',': 'comma',
         '=': 'equal', ';': 'semicolon'}


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


def wait_for(log, text, deadline):
    while time.time() < deadline:
        if os.path.exists(log):
            with open(log, 'rb') as f:
                if text.encode() in f.read():
                    return
        time.sleep(0.05)
    raise SystemExit(f'sendkeys: {text!r} never appeared in {log}')


def main():
    mon, log, keys = sys.argv[1:4]
    deadline = time.time() + float(sys.argv[4] if len(sys.argv) > 4 else 30)
    s = connect(mon, deadline)

    def press(name):
        s.sendall(f'sendkey {name}\n'.encode())
        time.sleep(KEY_DELAY_S)

    with open(keys) as f:
        for raw in f:
            line = raw.rstrip('\n')
            if not line.strip() or line.lstrip().startswith('#'):
                continue
            op, _, arg = line.partition(' ')
            if op == 'wait':
                wait_for(log, arg, deadline)
            elif op == 'type':
                for ch in arg:
                    press(key_name(ch))
            elif op == 'key':
                press(arg)
            else:
                raise SystemExit(f'sendkeys: unknown step {line!r}')
    s.close()


if __name__ == '__main__':
    main()
