#!/usr/bin/env python3
"""sendkeys.py - type into a QEMU guest's PS/2 keyboard through the monitor.

Usage: sendkeys.py <monitor-socket> <serial-log> <keys-file> [timeout-seconds]

The keys file is a small script, one step per line ('#' comments):
    wait <text>    block until <text> appears in the serial log after
                   what the previous wait matched
    type <text>    press each character (letters, digits, space, - . / , = ;)
    key <name>     press one QEMU key name (ret, backspace, ctrl-u, ...)
    screendump <f> save the screen as a PPM (checked by checkscreen.py)
    sleep <s>      pause (for a guest that shows progress only on screen)
    waitpixel <x> <y> <#rrggbb>
                   dump the screen until pixel (x, y) has that colour: for a
                   guest that shows progress only on screen, without a race
    mouse <dx> <dy> move the PS/2 mouse (screen directions: dy > 0 is down)
    click <button>  press and release left, right or middle
smoke.sh runs this in the background next to QEMU; a step that times out
exits 1 and smoke.sh then fails on its missing markers.
"""
import os
import socket
import sys
import time

KEY_DELAY_S = 0.02            # between keys: the guest drains the 8042 per IRQ
BUTTONS = {'left': 1, 'right': 2, 'middle': 4}   # QEMU's mouse_button mask
NAMES = {' ': 'spc', '-': 'minus', '.': 'dot', '/': 'slash', ',': 'comma',
         '=': 'equal', ';': 'semicolon', "'": 'apostrophe',
         '|': 'shift-backslash', '>': 'shift-dot', '<': 'shift-comma', '$': 'shift-4',
         '?': 'shift-slash', '"': 'shift-apostrophe', '_': 'shift-minus', '&': 'shift-7',
         '*': 'shift-8', '!': 'shift-1', ':': 'shift-semicolon',
         '(': 'shift-9', ')': 'shift-0', '\\': 'backslash',
         '%': 'shift-5', '#': 'shift-3', '+': 'shift-equal', '^': 'shift-6', '@': 'shift-2',
         '[': 'bracket_left', ']': 'bracket_right', '{': 'shift-bracket_left',
         '}': 'shift-bracket_right', '`': 'grave_accent', '~': 'shift-grave_accent'}


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


def pixel(path, x, y):
    """The colour of (x, y) in a binary PPM, as '#rrggbb'."""
    with open(path, 'rb') as f:
        data = f.read()
    fields, pos = [], 0
    while len(fields) < 4:  # P6, width, height, maxval
        while data[pos:pos + 1].isspace():
            pos += 1
        end = pos
        while not data[end:end + 1].isspace():
            end += 1
        fields.append(data[pos:end])
        pos = end
    width = int(fields[1])
    o = pos + 1 + (y * width + x) * 3
    return '#%02x%02x%02x' % tuple(data[o:o + 3])


def main():
    mon, log, keys = sys.argv[1:4]
    deadline = time.time() + float(sys.argv[4] if len(sys.argv) > 4 else 30)
    s = connect(mon, deadline)

    def dump(path):
        if os.path.exists(path):
            os.remove(path)
        s.sendall(f'screendump {path}\n'.encode())
        wait_for_file(path, deadline)

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
            elif op == 'mouse':
                dx, dy = arg.split()
                s.sendall(f'mouse_move {int(dx)} {int(dy)}\n'.encode())
                time.sleep(KEY_DELAY_S)
            elif op == 'click':
                for state in (BUTTONS[arg], 0):
                    s.sendall(f'mouse_button {state}\n'.encode())
                    time.sleep(0.1)
            elif op == 'sleep':
                time.sleep(float(arg))
            elif op == 'screendump':
                dump(os.path.abspath(arg))
            elif op == 'waitpixel':
                x, y, want = arg.split()
                probe = os.path.abspath(log + '.probe.ppm')
                while True:
                    dump(probe)
                    if pixel(probe, int(x), int(y)) == want.lower():
                        break
                    if time.time() > deadline:
                        raise SystemExit(f'sendkeys: pixel ({x},{y}) never became {want}')
                    time.sleep(0.2)
            else:
                raise SystemExit(f'sendkeys: unknown step {line!r}')
    s.close()


if __name__ == '__main__':
    main()
