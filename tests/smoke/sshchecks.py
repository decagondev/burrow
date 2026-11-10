"""sshchecks.py - the interactive and copying checks of tests/smoke/ssh.py
(W24-S03): two shells on pseudo-terminals at once (each its own pty, session
and process group), a window change seen by stty and a WINCH trap, ^C
interrupting a foreground sleep (130), exit returning the shell's status to the
host's ssh, and scp both ways with sha256 compared on both sides. Each check
goes through check(name, good, detail). Typed commands split their markers
with '' so that the echo of a line never matches what it waits for.
"""
import hashlib
import os
import time

PROMPT_S = 120  # a login: key exchange on TCG, then the shell


def _shell(h, name, check):
    """A logged-in shell whose prompt is 'S<name>> ', or None."""
    t = h.term("burrow")
    ok = t.expect(r"[$#] ", PROMPT_S) is not None
    if ok:
        t.send("PS1='S''%s> '\r" % name)
        ok = t.expect("S%s> " % name) is not None
    check("pty-shell-" + name, ok, t.text())
    return t if ok else None


def _ids(t):
    """(tty, process group, session) of the shell on t, or None."""
    t.send('echo T=$(tty) G=$(cut -d" " -f5 /proc/$$/stat) S=$(cut -d" " -f6 /proc/$$/stat)\r')
    m = t.expect(r"T=(/dev/pts/\d+) G=(\d+) S=(\d+)")
    return m.groups() if m else None


def _size(t):
    m = t.expect(r"\n(\d+) (\d+)\r?\n")
    return (int(m.group(1)), int(m.group(2))) if m else None


def interactive(h, check):
    a = _shell(h, "A", check)
    b = _shell(h, "B", check) if a else None
    try:
        if not b:
            return
        ia, ib = _ids(a), _ids(b)
        check("two-sessions", ia and ib and ia[0] != ib[0] and ia[1] != ib[1] and ia[2] != ib[2],
              "%r %r\n%s\n%s" % (ia, ib, a.text(), b.text()))
        a.expect("SA> ")
        a.send("stty size\r")
        before = _size(a)
        # SIGWINCH goes to the foreground group: a foreground sh -c traps it
        a.send("sh -c \"trap 'echo got-wi''nch' WINCH; sleep 6; stty size\"\r")
        a.expect("sleep 6")
        time.sleep(2)
        a.resize(40, 100)
        winch = a.expect("got-winch", 30) is not None
        after = _size(a)
        check("window-change", before == (24, 80) and winch and after == (40, 100),
              "before %r winch %r after %r\n%s" % (before, winch, after, a.text()))
        a.expect("SA> ")
        a.send("sleep 100\r")
        a.expect("sleep 100")
        time.sleep(3)
        start = time.time()
        a.send("\x03")
        back = a.expect("SA> ", 30) is not None
        a.send("echo st=$?\r")
        m = a.expect(r"st=(\d+)")
        check("interrupt", back and time.time() - start < 30 and m and m.group(1) == b"130", a.text())
        b.send("exit\r")
        sb = b.wait(60)
        a.send("exit 7\r")
        sa = a.wait(60)
        check("exit-status", sa == 7 and sb == 0, "A %r B %r\n%s" % (sa, sb, a.text()))
    finally:
        for t in (a, b):
            if t:
                t.kill()


def _sha(path):
    with open(path, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()


def copies(h, check, size_bytes=1 << 20):
    up = os.path.join(h.work, "up.bin")
    down = os.path.join(h.work, "down.bin")
    with open(up, "wb") as f:
        f.write(os.urandom(size_bytes))
    st, out = h.scp(up, "burrow@127.0.0.1:up.bin")
    st2, sums = h.ssh("burrow", "sha256sum up.bin; head -c %d /dev/urandom > made.bin; sha256sum made.bin" % size_bytes)
    lines = [l.split()[0] for l in sums.splitlines() if l.endswith(".bin")]
    check("scp-to-burrow", st == 0 and st2 == 0 and len(lines) == 2 and lines[0] == _sha(up), out + sums)
    if os.path.exists(down):
        os.remove(down)
    st, out = h.scp("burrow@127.0.0.1:made.bin", down)
    good = st == 0 and len(lines) == 2 and os.path.exists(down) and os.path.getsize(down) == size_bytes
    check("scp-from-burrow", good and _sha(down) == lines[1], out + sums)
