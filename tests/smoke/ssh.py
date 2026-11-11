#!/usr/bin/env python3
"""ssh.py - the host's side of make ssh-smoke (W24-S02, S03) and make
ssh-bench: the host's OpenSSH client logs in to dropbear on Burrow through a
hostfwd port, as a person would. It waits for the port, then: burrow logs in
with a password (an SSH_ASKPASS helper types it) and runs a command; a key
made here is put in burrow's ~/.ssh/authorized_keys over that login; a key
login runs id and ps; a wrong password is refused. S03 (sshchecks.py): two
interactive shells at once on pseudo-terminals, a window change, ^C, exit's
status, scp both ways. Last, it leaves /tmp/ssh-report and /tmp/ssh-done on
Burrow, which the console's keys file waits for and prints. The bench mode
installs the key, then times logins and scp and prints them (no budget).
Host keys are new on every run (dropbear -R makes them), so a throwaway
known-hosts file is used. Prints "SSH PASS" or "SSH FAIL: ..." (exit 1).

Usage: ssh.py <host-port> <work-dir> [bench]
"""
import os
import socket
import subprocess
import sys
import time

import sshchecks
from sshterm import Term

WAIT_S = 300    # Burrow boots and logs root in on the console first
CALL_S = 120    # one ssh session (key exchange on TCG is slow)
BENCH_LOGINS = 3
BENCH_BYTES = 4 << 20
BENCH_CIPHERS = ("chacha20-poly1305@openssh.com", "aes128-ctr")  # what dropbear offers


def wait_port(port):
    """Waits until dropbear answers with its banner (hostfwd accepts sooner)."""
    end = time.time() + WAIT_S
    while time.time() < end:
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=5) as s:
                s.settimeout(10)
                if s.recv(64).startswith(b"SSH-2.0-"):
                    return True
        except OSError:
            pass
        time.sleep(2)
    return False


class Host:
    def __init__(self, port, work):
        self.port, self.work = port, work
        self.key = os.path.join(work, "key")
        self.askpass = os.path.join(work, "askpass")
        self.known = os.path.join(work, "known_hosts")

    def _common(self):
        return ["-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=" + self.known,
                "-o", "ConnectTimeout=60", "-o", "LogLevel=ERROR"]

    def _keyonly(self):
        return ["-i", self.key, "-o", "BatchMode=yes", "-o", "PasswordAuthentication=no"]

    def _run(self, args, stdin=None, env=None):
        try:
            p = subprocess.run(args, input=stdin, capture_output=True, text=True, timeout=CALL_S, env=env)
        except subprocess.TimeoutExpired:
            return 124, "timed out"
        return p.returncode, p.stdout + p.stderr

    def ssh(self, user, command, password=None, stdin=None):
        """One session: (exit status, output)."""
        args = ["ssh", "-p", str(self.port)] + self._common()
        env = dict(os.environ)
        if password is None:
            args += self._keyonly()
        else:
            with open(self.askpass, "w") as f:
                f.write("#!/bin/sh\necho '%s'\n" % password)
            os.chmod(self.askpass, 0o700)
            args += ["-o", "PubkeyAuthentication=no", "-o", "PreferredAuthentications=password",
                     "-o", "NumberOfPasswordPrompts=1"]
            env.update(SSH_ASKPASS=self.askpass, SSH_ASKPASS_REQUIRE="force", DISPLAY=":0")
        return self._run(args + ["%s@127.0.0.1" % user, command], stdin, env)

    def term(self, user):
        """An interactive login (-tt) on a host pty, 24x80."""
        args = ["ssh", "-tt", "-p", str(self.port)] + self._common() + self._keyonly() + ["%s@127.0.0.1" % user]
        return Term(args, env=dict(os.environ, TERM="vt100"))

    def scp(self, src, dst, extra=()):
        """scp's original protocol (-O): dropbear has no SFTP."""
        return self._run(["scp", "-O", "-q", "-P", str(self.port)] + list(extra) + self._common() + self._keyonly() + [src, dst])

    def stream(self, path, cipher):
        """Seconds to send path's bytes into cat > /dev/null with cipher."""
        args = ["ssh", "-p", str(self.port), "-c", cipher] + self._common() + self._keyonly() + ["burrow@127.0.0.1", "cat > /dev/null"]
        start = time.time()
        with open(path, "rb") as f:
            subprocess.run(args, stdin=f, capture_output=True, timeout=CALL_S)
        return time.time() - start


def setup(h, check):
    st, out = h.ssh("burrow", "id -un; echo ssh: password'' login ran", password="burrow")
    check("password-login", st == 0 and "burrow" in out and "ssh: password login ran" in out, out)
    with open(h.key + ".pub") as f:
        pub = f.read()
    st, out = h.ssh("burrow", "mkdir -p .ssh && chmod 700 .ssh && cat > .ssh/authorized_keys && chmod 600 .ssh/authorized_keys",
                    password="burrow", stdin=pub)
    check("key-installed", st == 0, out)


def bench(h):
    """Prints login time and scp throughput; returns the report line."""
    start = time.time()
    for _ in range(BENCH_LOGINS):
        h.ssh("burrow", "true")
    login_s = (time.time() - start) / BENCH_LOGINS
    up = os.path.join(h.work, "bench.bin")
    with open(up, "wb") as f:
        f.write(os.urandom(BENCH_BYTES))
    start = time.time()
    st_up, _ = h.scp(up, "burrow@127.0.0.1:/tmp/bench.bin")
    up_s = time.time() - start
    start = time.time()
    st_down, _ = h.scp("burrow@127.0.0.1:/tmp/bench.bin", up + ".back")
    down_s = time.time() - start
    mb = BENCH_BYTES / 1e6
    print("SSH BENCH login %.2f s (mean of %d), scp %d MiB up %.2f MB/s (status %d), down %.2f MB/s (status %d)"
          % (login_s, BENCH_LOGINS, BENCH_BYTES >> 20, mb / up_s, st_up, mb / down_s, st_down))
    for cipher in BENCH_CIPHERS:  # the stream alone, by cipher: what the cipher costs on TCG
        print("SSH BENCH stream %s %.2f MB/s" % (cipher, mb / h.stream(up, cipher)))
    return "ssh: bench done"


def main():
    port, work = int(sys.argv[1]), sys.argv[2]
    is_bench = len(sys.argv) > 3 and sys.argv[3] == "bench"
    os.makedirs(work, exist_ok=True)
    h = Host(port, work)
    for f in (h.key, h.key + ".pub", h.known):
        if os.path.exists(f):
            os.remove(f)
    subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", h.key], check=True)
    if not wait_port(port):
        print("SSH FAIL: dropbear never answered on port %d" % port)
        return 1
    checks = []

    def check(name, good, detail):
        print("ssh: %s %s" % (name, "ok" if good else "FAIL"))
        if not good:
            print("  " + str(detail).strip().replace("\n", "\n  "))
        checks.append(bool(good))

    setup(h, check)
    if is_bench:
        report = bench(h)
    else:
        st, out = h.ssh("burrow", "id; ps")
        check("key-login", st == 0 and "uid=1000(burrow)" in out and "dropbear" in out, out)
        st, out = h.ssh("burrow", "echo should not run", password="wrong")
        check("wrong-password", st != 0 and "should not run" not in out, out)
        first = len(checks)
        report = "ssh: the host logged in by password and by key (%d of %d checks)" % (sum(checks), first)
        sshchecks.interactive(h, check)
        sshchecks.copies(h, check)
        report += "\nssh: interactive shells, a window change, ^C, exit status and scp both ways (%d of %d checks)" % (
            sum(checks[first:]), len(checks) - first)
    st, out = h.ssh("burrow", "printf '%%s\\n' '%s' > /tmp/ssh-report && touch /tmp/ssh-done" % report.replace("\n", "' '"))
    check("report", st == 0, out)
    if all(checks):
        print("SSH PASS (the host logged in to dropbear by password and by key, ran shells on ptys and copied files; "
              "a wrong password was refused)" if not is_bench else "SSH PASS (bench)")
        return 0
    print("SSH FAIL: %d of %d checks failed" % (len(checks) - sum(checks), len(checks)))
    return 1


if __name__ == "__main__":
    sys.exit(main())
