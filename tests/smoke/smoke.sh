#!/usr/bin/env bash
# smoke.sh - boot an image headless and assert ordered serial markers.
#
# Usage: tests/smoke/smoke.sh <disk.img> <expect-file> [timeout-seconds] [keys-file]
#
# With a keys file, QEMU gets a monitor socket and tests/smoke/sendkeys.py
# types the file's steps into the guest's PS/2 keyboard while it runs.
#
# PASS requires ALL of:
#   1. every non-comment line of <expect-file> appears in the serial log,
#      IN ORDER (a stage printing out of order is a bug too);
#   2. QEMU exits with status 33 = isa-debug-exit code 0x10 (success);
#   3. no line containing "PANIC" or "FAIL";
#   4. if <expect-file> with .screen for .txt exists, its pixel checks pass.
# The full serial log is kept in build/smoke-<name>.log for the bug tracker.
set -euo pipefail

IMG="$1"; EXPECT="$2"; TIMEOUT="${3:-20}"; KEYS="${4:-}"
QEMU="${QEMU:-qemu-system-x86_64}"
NAME="$(basename "$EXPECT" .txt)"
LOG="$(dirname "$IMG")/smoke-${NAME}.log"

MONITOR=()
rm -f "$LOG"
if [ -n "$KEYS" ]; then
  MON="$(mktemp -u "${TMPDIR:-/tmp}/burrow-qemu-${NAME}.XXXXXX")"   # not on DrvFs: no sockets there
  trap 'rm -f "$MON"' EXIT
  MONITOR=(-monitor "unix:$MON,server,nowait")
  python3 "$(dirname "$0")/sendkeys.py" "$MON" "$LOG" "$KEYS" "$TIMEOUT" &
fi

set +e
timeout "$TIMEOUT" "$QEMU" -machine pc -cpu qemu64 -m 128M \
  -drive "file=$IMG,format=raw,if=ide,index=0,media=disk" \
  -device isa-debug-exit,iobase=0xf4,iosize=0x04 \
  "${MONITOR[@]}" -serial "file:$LOG" -display none -no-reboot
STATUS=$?
[ -n "$KEYS" ] && wait
set -e

fail() { echo "SMOKE FAIL [$NAME]: $*"; echo "---- serial log ($LOG) ----"; cat "$LOG"; exit 1; }

[ "$STATUS" -eq 124 ] && fail "timed out after ${TIMEOUT}s"
tr -d '\r' < "$LOG" > "$LOG.clean"

if grep -qE 'PANIC|FAIL' "$LOG.clean"; then fail "panic/fail line found"; fi

line_no=0
while IFS= read -r want || [ -n "$want" ]; do
  [[ -z "$want" || "$want" == \#* ]] && continue
  found=$(tail -n +"$((line_no + 1))" "$LOG.clean" | grep -nF -m1 -- "$want" | cut -d: -f1 || true)
  [ -z "$found" ] && fail "missing (or out of order): '$want'"
  line_no=$((line_no + found))
done < "$EXPECT"

[ "$STATUS" -eq 33 ] || fail "QEMU exit status $STATUS, want 33 (debug-exit 0x10)"
# Screen checks: pixels of the keys file's screendumps (tests/smoke/checkscreen.py).
SCREEN="${EXPECT%.txt}.screen"
if [ -f "$SCREEN" ]; then
  python3 "$(dirname "$0")/checkscreen.py" "$SCREEN" || fail "screen does not match $SCREEN"
fi
echo "SMOKE PASS [$NAME] ($(grep -c . "$LOG.clean") log lines)"
