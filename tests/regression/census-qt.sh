#!/usr/bin/env bash
# census-qt.sh - W15-S01: which system calls do Qt programs make? Runs the
# static musl builds of qthello and qtwindow (build/qt-root, from make qt)
# on the host under strace -f and compares the set of syscall names with
# tests/regression/census-qt.txt, the way census.sh does for Go.
#
#   new name   -> FAIL: add it to census-qt.txt with its Linux number, and
#                 either implement it in kernel/proc or name the slice that
#                 will (kernel/proc's TestCensusQt checks the file)
#   name gone  -> fine (reported, not a failure)
#
# Burrow answers some calls differently from Linux (no /proc, no symbolic
# links), so Qt may take paths there that this census never sees; the
# kernel's "unimplemented syscall" log catches those.
#
# Usage: tests/regression/census-qt.sh [build-dir]
set -euo pipefail
B="${1:-build}"
QT="$B/qt-root"
EXPECT="tests/regression/census-qt.txt"
command -v strace >/dev/null || { echo "QT CENSUS FAIL: strace is not installed"; exit 1; }
[ -x "$QT/bin/qthello" ] || { echo "QT CENSUS SKIP (make qt builds Qt)"; exit 0; }

run() { # label, then the command
  local label=$1
  shift
  strace -f -qq -o "$B/census-qt-$label.strace" "$@" > "$B/census-qt-$label.out" 2>&1 ||
    { echo "QT CENSUS FAIL: $label exited with an error:"; cat "$B/census-qt-$label.out"; exit 1; }
}
# The child QProcess starts is Burrow's own static echo, not the host's
# glibc one, whose calls would say nothing about Qt.
[ -x "$B/user/echo" ] || { echo "QT CENSUS FAIL: $B/user/echo is missing (make go-smoke builds it)"; exit 1; }
QTHELLO_ECHO="$B/user/echo" run hello "$QT/bin/qthello"
export QT_QPA_PLATFORM=offscreen QT_QPA_FONTDIR="$QT/fonts" XDG_RUNTIME_DIR="$B/qt-run"
mkdir -p "$XDG_RUNTIME_DIR" && chmod 700 "$XDG_RUNTIME_DIR"
run png "$QT/bin/qtwindow" --png "$B/qtwindow.png"
run window "$QT/bin/qtwindow" --quit-after 300

sed -nE 's/^[0-9]+ +([a-z_][a-z0-9_]*)\(.*/\1/p' "$B"/census-qt-*.strace | grep -vx execve | sort -u > "$B/census-qt.seen"
grep -v '^#' "$EXPECT" | grep -v '# vdso' | awk 'NF {print $1}' | sort -u > "$B/census-qt.expected"
new=$(comm -23 "$B/census-qt.seen" "$B/census-qt.expected")
gone=$(comm -13 "$B/census-qt.seen" "$B/census-qt.expected")
[ -n "$gone" ] && echo "qt census: no longer used: $(echo $gone)"
if [ -n "$new" ]; then
  echo "QT CENSUS FAIL: new syscalls: $(echo $new)"
  echo "  add them to $EXPECT with their numbers (and implement them or name the slice)"
  exit 1
fi
echo "QT CENSUS PASS ($(wc -l < "$B/census-qt.seen") syscalls; kernel/proc's TestCensusQt checks each is served)"
