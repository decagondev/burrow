#!/usr/bin/env python3
"""fetch.py - the host's half of net-serve (W17-S05): fetches files from
servers inside Burrow through QEMU's forwarded ports and checks every byte.

Usage: fetch.py <httpd-port> <goserve-port> <dir> <file> <count>
       fetch.py bench <port> <dir> <file> <count>   (make net-bench)

Phase 1: busybox httpd on the first port: <count> short connections, each
fetching <file> and comparing it with <dir>/<file>; then /phase1-done,
which the server logs, so the keys file knows to restart it. Phase 2: the
restarted httpd (on the same port at once: SO_REUSEADDR) serves / instead
of /home, so home/<file> answers only once it is up; /phase2-done.
Phase 3: goserve (Go's net/http) on the second port, <count> fetches,
/phase3-done. Prints one line per phase and exits 1 on any
mismatch or timeout. The bench form is phase 1 alone, ending with
/bench-done. Talks to 127.0.0.1 only. Docs: WAVE-17-networking.md.
"""
import sys
import time
import urllib.error
import urllib.request

WAIT_S = 240  # Burrow boots, starts the server; then each wait is shorter


def get(port, path, timeout=10):
    """The body of http://127.0.0.1:port/path, or None if it failed."""
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{port}/{path}", timeout=timeout) as r:
            return r.read()
    except urllib.error.HTTPError as e:
        return b"" if e.code == 404 else None
    except (OSError, urllib.error.URLError):
        return None


def until(cond, seconds, what):
    end = time.time() + seconds
    while time.time() < end:
        if cond():
            return
        time.sleep(0.5)
    sys.exit(f"fetch: FAIL timed out waiting for {what}")


def many(port, name, want, count, label):
    until(lambda: get(port, name) == want, WAIT_S, f"{label} to serve {name}")
    start, good = time.time(), 0
    for _ in range(count):
        good += get(port, name) == want
    took = time.time() - start
    print(f"fetch: {label} {good} of {count} fetches of {name} match "
          f"({count / took:.1f} connections a second)", flush=True)
    if good != count:
        sys.exit(1)


def bench():
    port, root, name, count = int(sys.argv[2]), sys.argv[3], sys.argv[4], int(sys.argv[5])
    many(port, name, open(f"{root}/{name}", "rb").read(), count, "httpd")
    get(port, "bench-done")


def main():
    if sys.argv[1] == "bench":
        return bench()
    p1, p2, root, name, count = int(sys.argv[1]), int(sys.argv[2]), sys.argv[3], sys.argv[4], int(sys.argv[5])
    want = open(f"{root}/{name}", "rb").read()
    many(p1, name, want, count, "httpd")
    get(p1, "phase1-done")
    # the restarted httpd serves / instead of /home: only it has /home/<file>
    until(lambda: get(p1, f"home/{name}") == want, 60, "httpd to restart")
    print("fetch: httpd restarted on the same port", flush=True)
    get(p1, "phase2-done")
    many(p2, name, want, count, "goserve")
    get(p2, "phase3-done")
    print("fetch: ok", flush=True)


if __name__ == "__main__":
    main()
