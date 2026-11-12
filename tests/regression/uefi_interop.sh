#!/usr/bin/env bash
# uefi_interop.sh - mkimage's UEFI disk against the host's own tools (W26-S01).
#
# Usage: tests/regression/uefi_interop.sh <build-dir>
#
# sfdisk must read the GPT (one EFI system partition at 1 MiB), fsck.fat must
# find the partition clean, mtools must list the loader and the payloads in
# it, and objdump must see an EFI application with base relocations. Each
# check skips when its tool is not installed; it never weakens what it checks.
# Docs: docs/04-plan/waves/WAVE-26-uefi.md.
set -euo pipefail

BUILD="$1"
IMG="$BUILD/uefi.img"
EFI="$BUILD/efi/BOOTX64.EFI"
PATH="$PATH:/usr/sbin:/sbin"
fail() { echo "UEFI INTEROP FAIL: $*"; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }
ran=()

if have sfdisk; then
  dump="$(sfdisk -d "$IMG" 2>&1)" || fail "sfdisk: $dump"
  grep -q '^label: gpt' <<<"$dump" || fail "sfdisk sees no GPT: $dump"
  # sfdisk falls back to the backup header quietly when the primary is bad
  ! grep -qi 'corrupt\|mismatch' <<<"$dump" || fail "sfdisk: $(grep -i 'corrupt\|mismatch' <<<"$dump" | head -1)"
  grep -q 'start= *2048, .*type=C12A7328-F81F-11D2-BA4B-00A0C93EC93B' <<<"$dump" ||
    fail "no EFI system partition at 1 MiB: $dump"
  sfdisk --verify "$IMG" >/dev/null 2>&1 || fail "sfdisk --verify: $(sfdisk --verify "$IMG" 2>&1)"
  ran+=(sfdisk)
fi

if have fsck.fat; then
  part="$(mktemp)"; trap 'rm -f "$part"' EXIT
  cp "$BUILD/esp/uefi.fat" "$part"
  fsck.fat -n "$part" >/dev/null 2>&1 || fail "fsck.fat: $(fsck.fat -n "$part" 2>&1 | tail -3)"
  ran+=(fsck.fat)
fi

if have mdir; then
  for f in ::/EFI/BOOT/BOOTX64.EFI ::/burrow/manifest ::/burrow/nucleus; do
    MTOOLS_SKIP_CHECK=1 mdir -i "$IMG@@1M" "$f" >/dev/null 2>&1 || fail "mtools finds no $f in the partition"
  done
  MTOOLS_SKIP_CHECK=1 mlabel -s -i "$IMG@@1M" :: 2>/dev/null | grep -q BURROWBOOT || fail "the partition is not labelled BURROWBOOT"
  ran+=(mtools)
fi

if have objdump; then
  hdr="$(objdump -p "$EFI")"
  grep -q 'EFI application' <<<"$hdr" || fail "objdump: not an EFI application"
  grep -Eq 'Entry 5 [0-9a-f]+ 0*[1-9a-f][0-9a-f]* Base Relocation Directory' <<<"$hdr" || fail "objdump: no base relocations"
  ran+=(objdump)
fi

echo "UEFI INTEROP PASS (${ran[*]:-nothing installed to check with})"
