#!/usr/bin/env bash
# census.sh - PIT-09: did the Go toolchain start using a syscall the nucleus
# shim does not know? Builds tools/census on the host, runs it under strace,
# and compares the set of syscall names with tests/regression/census-go1.27.txt.
#
#   new name   -> FAIL: implement it in nucleus/shim*.c, boot go.img, then add
#                 it to the expected file (and the census doc)
#   name gone  -> fine (reported, not a failure)
#
# Usage: tests/regression/census.sh [build-dir]
set -euo pipefail
B="${1:-build}"
GO="${GO:-go}"
EXPECT="tests/regression/census-go1.27.txt"
mkdir -p "$B"

command -v strace >/dev/null || { echo "CENSUS FAIL: strace is not installed"; exit 1; }
(cd tools && CGO_ENABLED=0 "$GO" build -o "../$B/census" ./census)
GODEBUG=asyncpreemptoff=1 strace -f -c -o "$B/census.strace" "$B/census" >/dev/null

# strace -c rows end with the syscall name; skip headers, rules and the total.
awk 'NF >= 4 && $1 ~ /^[0-9.]+$/ && $NF != "total" { print $NF }' "$B/census.strace" \
  | grep -vx execve | sort -u > "$B/census.seen"
grep -v '^#' "$EXPECT" | sort -u > "$B/census.expected"

new=$(comm -23 "$B/census.seen" "$B/census.expected")
gone=$(comm -13 "$B/census.seen" "$B/census.expected")
[ -n "$gone" ] && echo "census: no longer used: $(echo $gone)"
if [ -n "$new" ]; then
  echo "CENSUS FAIL ($($GO version)): new syscalls: $(echo $new)"
  echo "  implement them in nucleus/shim*.c, check 'make go-smoke', then add them to $EXPECT"
  exit 1
fi
# Every syscall Go uses must have a row in the shim's table.
missing=""
for name in $(cat "$B/census.seen"); do
  grep -q "\[SYS_${name}\]" nucleus/shim.c || missing="$missing $name"
done
if [ -n "$missing" ]; then
  echo "CENSUS FAIL: used by Go but not in nucleus/shim.c's table:$missing"
  exit 1
fi
echo "CENSUS PASS ($($GO version | awk '{print $3}'), $(wc -l < "$B/census.seen") syscalls, all in the shim)"
