#!/usr/bin/env python3
"""poweroff.py - a real power-off (W20-S04): boots <img> with no debug-exit
device, waits for the console's prompt on the serial line, types poweroff,
and passes only if QEMU then exits by itself (ACPI S5 turns the machine off)
with status 0 and the nucleus said "acpi: powering off". Without ACPI the
console's poweroff would leave QEMU running and this would time out.

Usage: tests/smoke/poweroff.py <img> [timeout-seconds]
"""
import os
import subprocess
import sys
import threading
import time

img = sys.argv[1]
timeout = float(sys.argv[2]) if len(sys.argv) > 2 else 90
qemu = os.environ.get('QEMU', 'qemu-system-x86_64')
cpu = os.environ.get('QEMU_CPU', 'Broadwell-noTSX-IBRS')
cmd = [qemu, '-machine', 'pc', '-cpu', cpu, '-m', os.environ.get('MEM', '512M'),
       '-drive', 'file=%s,format=raw,if=ide,index=0,media=disk' % img,
       '-display', 'none', '-no-reboot', '-serial', 'stdio', '-monitor', 'none']
p = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
out = bytearray()


def reader():
    for chunk in iter(lambda: p.stdout.read1(4096), b''):
        out.extend(chunk)


threading.Thread(target=reader, daemon=True).start()
deadline = time.time() + timeout
typed = False
while time.time() < deadline and p.poll() is None:
    if not typed and b'ready: type on the keyboard' in out and out.rstrip().endswith(b'burrow>'):
        p.stdin.write(b'poweroff\r')
        p.stdin.flush()
        typed = True
    time.sleep(0.2)
if p.poll() is None:
    p.kill()
    print('POWEROFF FAIL: QEMU still running after %ds (typed: %s)' % (timeout, typed))
    print(out.decode(errors='replace')[-2000:])
    sys.exit(1)
time.sleep(0.3)
text = out.decode(errors='replace')
if p.returncode != 0 or '[nucleus] acpi: powering off' not in text:
    print('POWEROFF FAIL: QEMU exited with %d; "acpi: powering off" %s' % (p.returncode, 'seen' if 'powering off' in text else 'missing'))
    print(text[-2000:])
    sys.exit(1)
print('POWEROFF PASS (ACPI S5: QEMU turned off by itself)')
