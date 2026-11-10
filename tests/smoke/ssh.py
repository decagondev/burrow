#!/usr/bin/env python3
"""ssh.py - the host's side of make ssh-smoke (W24-S02): the host's OpenSSH
client logs in to dropbear on Burrow through a hostfwd port, as a person
would. It waits for the port, then: burrow logs in with a password (an
SSH_ASKPASS helper types it) and runs a command; a key made here is put in
burrow's ~/.ssh/authorized_keys over that login; a key login runs id and
ps; a wrong password is refused. Last, it leaves /tmp/ssh-report and
/tmp/ssh-done on Burrow, which the console's keys file waits for and prints.
Host keys are new on every run (dropbear -R makes them), so a throwaway
known-hosts file is used. Prints "SSH PASS" or "SSH FAIL: ..." (exit 1).

Usage: ssh.py <host-port> <work-dir>
"""
import os
import socket
import subprocess
import sys
import time

WAIT_S = 300    # Burrow boots and logs root in on the console first
CALL_S = 120    # one ssh session (key exchange on TCG is slow)


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

    def ssh(self, user, command, password=None, stdin=None):
        """One session: (exit status, output)."""
        args = ["ssh", "-p", str(self.port), "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=" + self.known,
                "-o", "ConnectTimeout=60", "-o", "LogLevel=ERROR"]
        env = dict(os.environ)
        if password is None:
            args += ["-i", self.key, "-o", "BatchMode=yes", "-o", "PasswordAuthentication=no"]
        else:
            with open(self.askpass, "w") as f:
                f.write("#!/bin/sh\necho '%s'\n" % password)
            os.chmod(self.askpass, 0o700)
            args += ["-o", "PubkeyAuthentication=no", "-o", "PreferredAuthentications=password",
                     "-o", "NumberOfPasswordPrompts=1"]
            env.update(SSH_ASKPASS=self.askpass, SSH_ASKPASS_REQUIRE="force", DISPLAY=":0")
        args += ["%s@127.0.0.1" % user, command]
        try:
            p = subprocess.run(args, input=stdin, capture_output=True, text=True, timeout=CALL_S, env=env)
        except subprocess.TimeoutExpired:
            return 124, "timed out"
        return p.returncode, p.stdout + p.stderr


def main():
    port, work = int(sys.argv[1]), sys.argv[2]
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
            print("  " + detail.strip().replace("\n", "\n  "))
        checks.append(good)

    st, out = h.ssh("burrow", "id -un; echo ssh: password'' login ran", password="burrow")
    check("password-login", st == 0 and "burrow" in out and "ssh: password login ran" in out, out)
    with open(h.key + ".pub") as f:
        pub = f.read()
    st, out = h.ssh("burrow", "mkdir -p .ssh && chmod 700 .ssh && cat > .ssh/authorized_keys && chmod 600 .ssh/authorized_keys",
                    password="burrow", stdin=pub)
    check("key-installed", st == 0, out)
    st, out = h.ssh("burrow", "id; ps")
    check("key-login", st == 0 and "uid=1000(burrow)" in out and "dropbear" in out, out)
    st, out = h.ssh("burrow", "echo should not run", password="wrong")
    check("wrong-password", st != 0 and "should not run" not in out, out)
    report = "ssh: the host logged in by password and by key (%d of %d checks)" % (sum(checks), len(checks))
    st, out = h.ssh("burrow", "echo '%s' > /tmp/ssh-report && touch /tmp/ssh-done" % report)
    check("report", st == 0, out)
    if all(checks):
        print("SSH PASS (the host logged in to dropbear by password and by key; a wrong password was refused)")
        return 0
    print("SSH FAIL: %d of %d checks failed" % (len(checks) - sum(checks), len(checks)))
    return 1


if __name__ == "__main__":
    sys.exit(main())
