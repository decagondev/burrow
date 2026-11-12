#!/usr/bin/env python3
"""unix-desktop.py - the host's side of make unix-desktop-smoke (W25-S05):
burrow, a user, runs the desktop on the ext2 root, and the host checks it
over ssh. It waits for dropbear (root started it through su on the console),
puts a key in burrow's authorized_keys with a password login, then, logged in
by key, waits until burrow's session has made /tmp/desk-go and ps lists the
X server, dwm, st and NetSurf, with dwm, st and NetSurf burrow's (the X
server is set-user-id root, so its effective user is root), and NetSurf's log
says the page was laid out. It leaves its report in /tmp/desk-report and
ends the session the way a person would (pkill dwm, which burrow may signal:
it is burrow's), so startx stops the server and the console prints the
report. Prints "UNIX DESKTOP PASS" or "UNIX DESKTOP FAIL: ..." (exit 1).

Usage: unix-desktop.py <host-port> <work-dir>
"""
import os
import subprocess
import sys
import time

from ssh import Host, setup, wait_port

DESK_WAIT_S = 400  # X, dwm, st and NetSurf start on TCG
POLL_S = 5
PROGRAMS = ("Xfbdev", "dwm", "st", "netsurf")
USERS = {"Xfbdev": "root", "dwm": "burrow", "st": "burrow", "netsurf": "burrow"}


def owners(ps):
    """The user of each of PROGRAMS in busybox ps's output (PID USER VSZ STAT COMMAND)."""
    found = {}
    for line in ps.splitlines():
        fields = line.split()
        if len(fields) < 5 or not fields[0].isdigit():
            continue
        name = os.path.basename(fields[4])
        if name in PROGRAMS:
            found.setdefault(name, fields[1])
    return found


def main():
    port, work = int(sys.argv[1]), sys.argv[2]
    os.makedirs(work, exist_ok=True)
    h = Host(port, work)
    for f in (h.key, h.key + ".pub", h.known):
        if os.path.exists(f):
            os.remove(f)
    subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", h.key], check=True)
    if not wait_port(port):
        print("UNIX DESKTOP FAIL: dropbear never answered on port %d" % port)
        return 1
    failed = []

    def check(name, good, detail):
        print("unix-desktop: %s %s" % (name, "ok" if good else "FAIL"))
        if not good:
            print("  " + str(detail).strip().replace("\n", "\n  "))
            failed.append(name)

    setup(h, check)
    end = time.time() + DESK_WAIT_S
    found, out, laid = {}, "", False
    while time.time() < end:
        st, out = h.ssh("burrow", "test -f /tmp/desk-go && ps && grep -c html_box_convert_done /tmp/ns.log")
        found = owners(out)
        laid = st == 0 and out.strip().splitlines()[-1].isdigit()
        if len(found) == len(PROGRAMS) and laid:
            break
        time.sleep(POLL_S)
    check("programs", found == USERS, "want %s, ps says %s:\n%s" % (USERS, found, out))
    check("page", laid, out)
    report = "desk: the host over ssh sees Xfbdev as root, dwm, st and netsurf as burrow"
    if failed:
        report = "desk: the host over ssh saw %s" % found
    st, out = h.ssh("burrow", "echo '%s' > /tmp/desk-report && pkill dwm" % report)
    check("report", st == 0, out)
    if failed:
        print("UNIX DESKTOP FAIL: %s" % ", ".join(failed))
        return 1
    print("UNIX DESKTOP PASS (burrow ran X, dwm, st and NetSurf on the ext2 root; the host saw them over ssh)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
