#!/usr/bin/env python3
"""desktop-ssh.py - the host's side of desktop-smoke's ssh login (W24-S06):
while the desktop runs on Burrow, the host's OpenSSH logs in to dropbear as
root with the key tests/smoke/desktop-ssh.sh made (its public half is in
/home/.ssh on the desktop's home disk). It waits for dropbear, then until
/tmp/ssh-go exists (a terminal on the desktop makes it, so the login comes
at a known step), then checks that ps lists the X server and dwm and says
so on Burrow's console (/dev/tty1, which reaches the serial line while X
owns the screen). root's shell on go-dev.img is user/sh.c, so the commands
run under busybox sh. Prints "DESKTOP SSH PASS" or "... FAIL" (exit 1).

Usage: desktop-ssh.py <host-port> <work-dir>
"""
import sys
import time

from ssh import Host, wait_port

GO_WAIT_S = 600  # the desktop's steps before the terminal makes /tmp/ssh-go
POLL_S = 3


def main():
    h = Host(int(sys.argv[1]), sys.argv[2])
    if not wait_port(h.port):
        print("DESKTOP SSH FAIL: dropbear never answered")
        return 1
    end = time.time() + GO_WAIT_S
    st, out = 1, ""
    while time.time() < end:
        st, out = h.ssh("root", "/bin/busybox sh -c 'test -f /tmp/ssh-go && /bin/busybox ps'")
        if st == 0:
            break
        time.sleep(POLL_S)
    if st != 0 or "Xfbdev" not in out or "dwm" not in out:
        print("DESKTOP SSH FAIL: no X server and dwm seen over ssh:\n" + out)
        return 1
    st, out = h.ssh("root", "/bin/busybox echo desktop: the host over ssh sees Xfbdev and dwm > /dev/tty1")
    if st != 0:
        print("DESKTOP SSH FAIL: the report did not reach the console:\n" + out)
        return 1
    print("DESKTOP SSH PASS (the host logged in over ssh while the desktop ran; ps lists Xfbdev and dwm)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
