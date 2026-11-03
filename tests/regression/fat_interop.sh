#!/usr/bin/env bash
# fat_interop.sh - kernel/fs/fat against the Linux FAT tools, both ways:
#   1. fsck.fat accepts the image mkfat built (build/data.img), and mtools
#      reads the same files, long names included;
#   2. mkfat -cat (the kernel's FAT reader) reads an image that mkfs.fat
#      made and mtools filled;
#   3. the kernel's FAT writer (mkfat -put/-mkdir/-rm, W13-S04) changes both
#      images, and fsck.fat accepts them and mtools reads what was written.
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
out=$(fsck.fat -n "$B/data.img" 2>&1) || { echo "$out"; fail "fsck.fat rejects $B/data.img"; }
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

# 3. Our writer, Linux tools: the same edits on Linux's image (4 KiB clusters)
#    and on a copy of ours (512-byte clusters).
mkfs.fat -F 32 -S 512 -s 8 -n LINUXFAT -C "$img.big" $((300 * 1024)) >/dev/null
cp "$B/data.img" "$B/data-edited.img"
work="$(mktemp -d)"
seq 1 20000 > "$work/big.txt"         # about 108 KiB: many clusters
echo 'int main(void) { return 0; }' > "$work/main.c"
echo small > "$work/small.txt"
for target in "$(realpath "$img.big")" "$(realpath "$B/data-edited.img")"; do
  mkfat -mkdir "$target" src
  mkfat -mkdir "$target" src/deep
  mkfat -put "$target" "src/deep/A long file name.c" "$work/main.c"
  mkfat -put "$target" big.txt "$work/big.txt"
  mkfat -put "$target" big.txt "$work/small.txt"   # shrink: clusters go back
  mkfat -put "$target" grows.txt "$work/small.txt"
  mkfat -put "$target" grows.txt "$work/big.txt"   # grow
  mkfat -put "$target" gone.txt "$work/small.txt"
  mkfat -rm "$target" gone.txt
  for i in $(seq 1 30); do mkfat -put "$target" "src/file $i.txt" "$work/small.txt"; done   # the directory grows
  out=$(fsck.fat -n "$target" 2>&1) || { echo "$out"; fail "fsck.fat rejects what the kernel wrote ($target)"; }
  [ "$(mtype -i "$target" "::/src/deep/A long file name.c")" = "$(cat "$work/main.c")" ] || fail "mtools: long name in a new directory"
  [ "$(mtype -i "$target" ::/big.txt)" = "small" ] || fail "mtools: a shrunk file"
  cmp -s <(mtype -i "$target" ::/grows.txt) "$work/big.txt" || fail "mtools: a grown file"
  mtype -i "$target" ::/gone.txt >/dev/null 2>&1 && fail "mtools still finds a removed file"
  [ "$(mdir -i "$target" -b ::/src | wc -l)" -eq 31 ] || fail "mtools: 30 files and a directory in src"
done
rm -rf "$work"
echo "FAT INTEROP PASS ($(mkfat -ls "$img" | wc -l) files read from a mkfs.fat image; kernel writes pass fsck.fat on 2 images)"
