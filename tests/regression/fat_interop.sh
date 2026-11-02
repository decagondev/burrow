#!/usr/bin/env bash
# fat_interop.sh - kernel/fs/fat against the Linux FAT tools, both ways:
#   1. fsck.fat accepts the image mkfat built (build/data.img), and mtools
#      reads the same files, long names included;
#   2. mkfat -cat (the kernel's FAT reader) reads an image that mkfs.fat
#      made and mtools filled.
# Needs dosfstools and mtools (CI installs them); skips without them.
#
# Usage: tests/regression/fat_interop.sh [build-dir]
set -euo pipefail
B="${1:-build}"
GO="${GO:-go}"
if ! command -v fsck.fat >/dev/null || ! command -v mcopy >/dev/null; then
  echo "FAT INTEROP SKIP (install dosfstools and mtools to run it)"
  exit 0
fi
export MTOOLS_SKIP_CHECK=1
fail() { echo "FAT INTEROP FAIL: $*"; exit 1; }
mkfat() { (cd kernel && "$GO" run ./cmd/mkfat "$@"); }

# 1. Our image, Linux tools.
fsck.fat -n "$B/data.img" >/dev/null || fail "fsck.fat rejects $B/data.img"
got=$(mtype -i "$B/data.img" "::/docs/A-Long-File-Name.text") || fail "mtools cannot read a long name"
[ "$got" = "$(cat 'datadisk/docs/A-Long-File-Name.text')" ] || fail "mtools read other bytes"

# 2. Linux's image, our reader.
img="$(realpath "$B")/mkfs-fat.img"
rm -f "$img"
mkfs.fat -F 32 -S 512 -s 1 -n LINUXFAT -C "$img" $((40 * 1024)) >/dev/null
mmd -i "$img" ::/docs
mcopy -i "$img" datadisk/hello.txt ::/HELLO.TXT
mcopy -i "$img" "datadisk/docs/A-Long-File-Name.text" "::/docs/A-Long-File-Name.text"
[ "$(mkfat -cat "$img" HELLO.TXT)" = "$(cat datadisk/hello.txt)" ] || fail "short name from mtools"
[ "$(mkfat -cat "$img" docs/A-Long-File-Name.text)" = "$(cat 'datadisk/docs/A-Long-File-Name.text')" ] ||
  fail "long name from mtools"
echo "FAT INTEROP PASS ($(mkfat -ls "$img" | wc -l) files read from a mkfs.fat image)"
