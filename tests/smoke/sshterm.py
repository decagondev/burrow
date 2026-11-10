"""sshterm.py - an interactive ssh session on a host pseudo-terminal, for
tests/smoke/ssh.py (W24-S03). The host's ssh runs with -tt on a pty this
module owns, as in a terminal window: the window size is set before ssh
starts and changed with resize (the host's pty then signals SIGWINCH to ssh,
which sends a window change to dropbear); send types, expect waits for a
pattern in what came back. Never interprets the output beyond the patterns.
"""
import fcntl
import os
import pty
import re
import select
import signal
import struct
import termios
import time


def _winsize(fd, rows, cols):
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))


class Term:
    def __init__(self, args, rows=24, cols=80, env=None):
        pid, fd = pty.fork()
        if pid == 0:
            _winsize(0, rows, cols)
            os.execvpe(args[0], args, env or os.environ)
        self.pid, self.fd, self.buf, self.log = pid, fd, b"", b""
        self.status = None

    def resize(self, rows, cols):
        _winsize(self.fd, rows, cols)

    def send(self, text):
        os.write(self.fd, text.encode())

    def _fill(self, end):
        left = end - time.time()
        if left <= 0:
            return False
        r, _, _ = select.select([self.fd], [], [], left)
        if not r:
            return False
        try:
            data = os.read(self.fd, 4096)
        except OSError:  # EIO: ssh closed its side
            data = b""
        if not data:
            raise EOFError
        self.buf += data
        self.log += data
        return True

    def expect(self, pattern, timeout_s=60):
        """The first match of pattern (a regex), or None at the timeout or EOF;
        what came before and with it is consumed."""
        rx = re.compile(pattern.encode())
        end = time.time() + timeout_s
        while True:
            m = rx.search(self.buf)
            if m:
                self.buf = self.buf[m.end():]
                return m
            try:
                if not self._fill(end):
                    return None
            except EOFError:
                return None

    def wait(self, timeout_s=60):
        """ssh's exit status once it ends (reading until it closes), else None."""
        end = time.time() + timeout_s
        while time.time() < end:
            try:
                self._fill(min(end, time.time() + 1))
            except EOFError:
                pass
            done, st = os.waitpid(self.pid, os.WNOHANG)
            if done:
                self.status = os.waitstatus_to_exitcode(st)
                os.close(self.fd)
                return self.status
        return None

    def kill(self):
        if self.status is None:
            try:
                os.kill(self.pid, signal.SIGKILL)
                os.waitpid(self.pid, 0)
            except OSError:
                pass
            self.status = -9

    def text(self):
        return self.log.decode(errors="replace")
