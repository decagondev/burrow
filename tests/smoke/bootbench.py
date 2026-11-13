#!/usr/bin/env python3
"""bootbench.py - time a boot from the firmware's first line to the Go
kernel (W26-S06, make boot-bench).

Usage: bootbench.py <label> <run> <timeout-seconds> <log-dir> -- <qemu command...>

Boots the QEMU command once (the Makefile calls it once per run, giving
OVMF a fresh variable store each time, as smoke.sh does) with the serial
line on a pipe (`-serial stdio` is added), stamps the bytes as they arrive
and stops QEMU once `BURROW:GO:OK` is seen. The stamped log is kept as
<log-dir>/boot-bench-<label>-<run>.log ("<ms> <line>"). It prints one
line that tests/smoke/boot-budgets.py reads:

  boot: <label> run <run> first <ms> burrow <ms> nucleus <ms> nucleus-ok <ms> go <ms>

every time in milliseconds after QEMU was started: the firmware's first
byte on COM1 (OVMF writes its console's set-up; SeaBIOS writes nothing, so
on the BIOS path it is stage1's line), Burrow's first line (`[stage1]`,
or the UEFI loader's `Burrow: UEFI loader`), the nucleus's first line,
`BURROW:NUCLEUS:OK` and `BURROW:GO:OK`. A boot that misses one of them, or
the time limit, fails the bench (exit 1). It never judges the numbers.
Docs: docs/04-plan/waves/WAVE-26-uefi.md (S06).
"""
import os
import subprocess
import sys
import time

MARKS = (                     # (name, how a line starts or what it holds)
    ("burrow", ("[stage1]", "Burrow: UEFI loader")),
    ("nucleus", ("[nucleus]",)),
    ("nucleus-ok", ("BURROW:NUCLEUS:OK",)),
    ("go", ("BURROW:GO:OK",)),
)


def mark_of(line, seen):
    """The first mark this line reaches that has not been seen yet."""
    for name, starts in MARKS:
        if name in seen:
            continue
        if any(line.startswith(s) or (s.startswith("BURROW:") and s in line) for s in starts):
            return name
    return None


def one_boot(cmd, timeout_s, log_path):
    """Boots once; returns {mark: ms after launch} or an error string."""
    start = time.monotonic()
    times = {}
    q = subprocess.Popen(cmd + ["-serial", "stdio"], stdin=subprocess.DEVNULL,
                         stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    os.set_blocking(q.stdout.fileno(), True)
    pending = b""
    with open(log_path, "w") as log:
        try:
            while "go" not in times:
                if time.monotonic() - start > timeout_s:
                    return f"timed out after {timeout_s} s"
                chunk = os.read(q.stdout.fileno(), 4096)
                now_ms = (time.monotonic() - start) * 1000
                if not chunk:
                    return f"QEMU exited ({q.wait()}) before BURROW:GO:OK"
                times.setdefault("first", now_ms)
                pending += chunk
                while b"\n" in pending:
                    raw, pending = pending.split(b"\n", 1)
                    line = raw.decode(errors="replace").replace("\r", "")
                    log.write(f"{now_ms:9.1f} {line}\n")
                    name = mark_of(line, times)
                    if name:
                        times[name] = now_ms
        finally:
            q.kill()
            q.wait()
    return times


def main():
    sep = sys.argv.index("--")
    label, n, timeout_s, log_dir = sys.argv[1], int(sys.argv[2]), float(sys.argv[3]), sys.argv[4]
    t = one_boot(sys.argv[sep + 1:], timeout_s, os.path.join(log_dir, f"boot-bench-{label}-{n}.log"))
    if isinstance(t, str):
        print(f"BOOT BENCH FAIL [{label} run {n}]: {t}")
        return 1
    missing = [m for m, _ in MARKS if m not in t]
    if missing:
        print(f"BOOT BENCH FAIL [{label} run {n}]: no {', '.join(missing)}")
        return 1
    print(f"boot: {label} run {n} first {t['first']:.0f} burrow {t['burrow']:.0f} "
          f"nucleus {t['nucleus']:.0f} nucleus-ok {t['nucleus-ok']:.0f} go {t['go']:.0f}", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
