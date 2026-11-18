#!/usr/bin/env bash
# smoke.sh - boot an image headless and assert ordered serial markers.
#
# Usage: tests/smoke/smoke.sh <disk.img> <expect-file> [timeout-seconds] [keys-file]
#
# With a keys file, QEMU gets a monitor socket and tests/smoke/sendkeys.py
# types the file's steps into the guest's PS/2 keyboard while it runs.
# QEMU_EXTRA adds QEMU arguments (make go-smoke attaches build/data.img);
# MACHINE picks QEMU's machine (pc; q35 for make q35-smoke, W27-S01);
# BOOT_IF=usb puts <disk.img> on a USB stick (usb-storage behind an xHCI
# controller of its own, the firmware's boot device) instead of IDE (W27-S05);
# BOOT_IF=usbhub puts that stick on port 1 of a USB hub on that controller,
# whose other ports QEMU_EXTRA may fill (bus bootxhci.0, port 1.N; W28-S05;
# no bootindex there: OVMF cannot match QEMU's boot path through a hub and
# would keep only its shell, while its own entry for the removable stick
# boots it);
# MEM is the guest's RAM (default 128M; the Go images use 512M, W14-S01).
# SMP is the guest's CPU count (W20-S07; the Makefile exports 4 since S08,
# unset here means one), on multi-threaded TCG.
# A guest reset ends the run (-no-reboot: a crash that resets must not loop
# until the timeout) unless ALLOW_REBOOT=1, for make reboot-smoke (W28-S04),
# where QEMU boots the same disk again.
#
# PASS requires ALL of:
#   1. every non-comment line of <expect-file> appears in the serial log,
#      IN ORDER (a stage printing out of order is a bug too);
#   2. QEMU exits with status 33 = isa-debug-exit code 0x10 (success), or
#      with POWEROFF=acpi (W26-S04) there is no debug-exit device and QEMU
#      must exit 0 by itself: the guest turned the machine off through ACPI;
#   3. no line containing "PANIC" or "FAIL";
#   4. if <expect-file> with .screen for .txt exists, its pixel checks pass.
# The full serial log is kept in build/smoke-<name>.log for the bug tracker
# (<name> is the expect file's, or SMOKE_NAME's when one expect file serves
# two boots: W26-S05's UEFI runs of the BIOS smokes).
set -euo pipefail

IMG="$1"; EXPECT="$2"; TIMEOUT="${3:-20}"; KEYS="${4:-}"
QEMU="${QEMU:-qemu-system-x86_64}"
NAME="${SMOKE_NAME:-$(basename "$EXPECT" .txt)}"
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

BOOT=(-drive "file=$IMG,format=raw,if=ide,index=0,media=disk")
if [ "${BOOT_IF:-ide}" = usb ]; then
  BOOT=(-device qemu-xhci,id=bootxhci -drive "file=$IMG,format=raw,if=none,id=bootstick"
        -device usb-storage,bus=bootxhci.0,drive=bootstick,bootindex=0)
elif [ "${BOOT_IF:-ide}" = usbhub ]; then
  BOOT=(-device qemu-xhci,id=bootxhci -device usb-hub,bus=bootxhci.0,port=1 -drive "file=$IMG,format=raw,if=none,id=bootstick"
        -device usb-storage,bus=bootxhci.0,port=1.1,drive=bootstick)   # no bootindex: OVMF matches no hub path
fi

NO_REBOOT=(-no-reboot)
[ "${ALLOW_REBOOT:-0}" = 1 ] && NO_REBOOT=()
set +e
timeout "$TIMEOUT" "$QEMU" -machine "${MACHINE:-pc}" -cpu "${QEMU_CPU:-Broadwell-noTSX-IBRS}" -m "${MEM:-128M}" -smp "${SMP:-1}" -accel tcg,thread=multi \
  "${FIRMWARE[@]}" "${BOOT[@]}" \
  "${DEBUG_EXIT[@]}" \
  "${MONITOR[@]}" ${QEMU_EXTRA:-} -serial "file:$LOG" -display none "${NO_REBOOT[@]}"
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
