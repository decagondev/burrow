#!/usr/bin/env bash
# smoke.sh - boot an image headless and assert ordered serial markers.
#
# Usage: tests/smoke/smoke.sh <disk.img> <expect-file> [timeout-seconds] [keys-file]
#
# With a keys file, QEMU gets a monitor socket and tests/smoke/sendkeys.py
# types the file's steps into the guest's PS/2 keyboard while it runs.
# QEMU_EXTRA adds QEMU arguments (make go-smoke attaches build/data.img);
# MEM is the guest's RAM (default 128M; the Go images use 512M, W14-S01).
# SMP is the guest's CPU count (W20-S07; the Makefile exports 4 since S08,
# unset here means one), on multi-threaded TCG.
#
# PASS requires ALL of:
#   1. every non-comment line of <expect-file> appears in the serial log,
#      IN ORDER (a stage printing out of order is a bug too);
#   2. QEMU exits with status 33 = isa-debug-exit code 0x10 (success), or
#      with POWEROFF=acpi (W26-S04) there is no debug-exit device and QEMU
#      must exit 0 by itself: the guest turned the machine off through ACPI;
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
  QMP="$MON.qmp"   # QMP too, for absolute pointer events (W22-S05)
  trap 'rm -f "$MON" "$QMP"' EXIT
  MONITOR=(-monitor "unix:$MON,server,nowait" -qmp "unix:$QMP,server,nowait")
  # sendkeys gives up a little before QEMU is killed, so the kernel's dump
  # (SysRq, W20-S01) of a hang still reaches the log
  python3 "$(dirname "$0")/sendkeys.py" "$MON" "$LOG" "$KEYS" "$((TIMEOUT > 12 ? TIMEOUT - 6 : TIMEOUT))" "$QMP" &
fi

# W26: with OVMF_CODE and OVMF_VARS set, UEFI firmware (OVMF) boots the image
# instead of SeaBIOS; each boot gets a fresh copy of the variable store.
FIRMWARE=()
if [ -n "${OVMF_CODE:-}" ]; then
  cp "$OVMF_VARS" "$LOG.vars"
  FIRMWARE=(-drive "if=pflash,format=raw,unit=0,readonly=on,file=$OVMF_CODE"
            -drive "if=pflash,format=raw,unit=1,file=$LOG.vars")
fi

# POWEROFF=acpi: no debug-exit device, so success is ACPI S5 turning QEMU off.
DEBUG_EXIT=(-device isa-debug-exit,iobase=0xf4,iosize=0x04); WANT_STATUS=33
if [ "${POWEROFF:-}" = acpi ]; then DEBUG_EXIT=(); WANT_STATUS=0; fi

set +e
timeout "$TIMEOUT" "$QEMU" -machine pc -cpu "${QEMU_CPU:-Broadwell-noTSX-IBRS}" -m "${MEM:-128M}" -smp "${SMP:-1}" -accel tcg,thread=multi \
  "${FIRMWARE[@]}" -drive "file=$IMG,format=raw,if=ide,index=0,media=disk" \
  "${DEBUG_EXIT[@]}" \
  "${MONITOR[@]}" ${QEMU_EXTRA:-} -serial "file:$LOG" -display none -no-reboot
STATUS=$?
[ -n "$KEYS" ] && wait
set -e

fail() { echo "SMOKE FAIL [$NAME]: $*"; echo "---- serial log ($LOG) ----"; cat "$LOG"; exit 1; }

[ "$STATUS" -eq 124 ] && fail "timed out after ${TIMEOUT}s"
tr -d '\r' < "$LOG" > "$LOG.clean"

if grep -aqE 'PANIC|FAIL' "$LOG.clean"; then fail "panic/fail line found"; fi

line_no=0
while IFS= read -r want || [ -n "$want" ]; do
  [[ -z "$want" || "$want" == \#* ]] && continue
  found=$(tail -n +"$((line_no + 1))" "$LOG.clean" | grep -anF -m1 -- "$want" | cut -d: -f1 || true)
  [ -z "$found" ] && fail "missing (or out of order): '$want'"
  line_no=$((line_no + found))
done < "$EXPECT"

[ "$STATUS" -eq "$WANT_STATUS" ] || fail "QEMU exit status $STATUS, want $WANT_STATUS (33: debug-exit 0x10; 0: ACPI power-off)"
# Screen checks: pixels of the keys file's screendumps (tests/smoke/checkscreen.py).
SCREEN="${EXPECT%.txt}.screen"
if [ -f "$SCREEN" ]; then
  python3 "$(dirname "$0")/checkscreen.py" "$SCREEN" || fail "screen does not match $SCREEN"
fi
echo "SMOKE PASS [$NAME] ($(grep -c . "$LOG.clean") log lines)"
