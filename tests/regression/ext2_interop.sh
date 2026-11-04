#!/usr/bin/env bash
# ext2_interop.sh - kernel/fs/ext2 against e2fsprogs (W19-S01): the
# package's tests build every volume they read with mke2fs -d, set owners
# with debugfs, index a directory with e2fsck -D, and check each volume with
# e2fsck -fn; here they run with BURROW_EXT2_REQUIRED set, so a missing tool
# fails instead of skipping. Skips without e2fsprogs (CI has it).
# S02 adds the writer's half: what the kernel wrote, e2fsck accepts.
#
# Usage: tests/regression/ext2_interop.sh
set -euo pipefail
GO="${GO:-go}"
PATH="$PATH:/usr/sbin:/sbin"
if ! command -v mke2fs >/dev/null || ! command -v e2fsck >/dev/null || ! command -v debugfs >/dev/null; then
  echo "EXT2 INTEROP SKIP (install e2fsprogs to run it)"
  exit 0
fi
out=$(cd kernel && BURROW_EXT2_REQUIRED=1 "$GO" test -count=1 -v ./fs/ext2 2>&1) || { echo "$out"; echo "EXT2 INTEROP FAIL"; exit 1; }
echo "EXT2 INTEROP PASS ($(grep -c -- '--- PASS' <<<"$out") tests on volumes from mke2fs $(mke2fs -V 2>&1 | head -1 | cut -d' ' -f2))"
