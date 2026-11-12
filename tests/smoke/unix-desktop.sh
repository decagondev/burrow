#!/usr/bin/env bash
# unix-desktop.sh - the desktop as a user (W25-S05, make unix-desktop-smoke):
# boots <img> (go-unix.img: login on the console) on <root> (an ext2 root
# disk made with the set-user-id X server, as hdb) with the NETDATA disk as
# hdc, the X disk as hdd and a free host port forwarded to port 22. The
# console's keys file logs burrow in, starts dropbear through su and runs
# startx; meanwhile tests/smoke/unix-desktop.py checks the session over ssh
# and ends it. The host's keys live on a Linux file system: OpenSSH refuses a
# private key /mnt/c shows as everyone's.
#
# Usage: tests/smoke/unix-desktop.sh <build-dir> <img> <root.img> [timeout]
set -euo pipefail
B="$1" IMG="$2" ROOT="$3" TIMEOUT="${4:-600}"
WORK="${NET_CACHE:-$HOME/.cache/burrow-net}/unix-desktop"
P=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
python3 tests/smoke/unix-desktop.py "$P" "$WORK" > "$B/unix-desktop-host.log" 2>&1 &
HOST=$!
trap 'kill $HOST 2>/dev/null || true' EXIT
QEMU_EXTRA="-drive file=$ROOT,format=raw,if=ide,index=1,media=disk -drive file=$B/net-data.img,format=raw,if=ide,index=2,media=disk -drive file=$B/x11-data.img,format=raw,if=ide,index=3,media=disk -nic user,model=${NIC_MODEL:-e1000},hostfwd=tcp:127.0.0.1:$P-:22" \
  tests/smoke/smoke.sh "$IMG" tests/smoke/expect/unix-desktop.txt "$TIMEOUT" tests/smoke/keys/unix-desktop.keys
status=0
wait $HOST || status=$?
cat "$B/unix-desktop-host.log"
exit $status
