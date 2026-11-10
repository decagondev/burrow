#!/usr/bin/env bash
# ssh-smoke.sh - the host logs in to Burrow over ssh (W24-S02, make
# ssh-smoke): boots <img> (go-unix.img: login on the console) on <root>
# (an ext2 root disk, as hdb) with the NETDATA disk as hdc and a free host
# port forwarded to port 22. The console's keys file logs root in, starts
# dropbear and waits for /tmp/ssh-done; meanwhile tests/smoke/ssh.py logs in
# from the host (password, key, a refusal) and leaves that file. The work
# directory for the host's keys is on a Linux file system: OpenSSH refuses a
# private key /mnt/c shows as everyone's.
#
# Usage: tests/smoke/ssh-smoke.sh <build-dir> <img> <root.img> [timeout]
set -euo pipefail
B="$1" IMG="$2" ROOT="$3" TIMEOUT="${4:-300}"
WORK="${NET_CACHE:-$HOME/.cache/burrow-net}/ssh-smoke"
P=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
python3 tests/smoke/ssh.py "$P" "$WORK" > "$B/ssh-host.log" 2>&1 &
HOST=$!
trap 'kill $HOST 2>/dev/null || true' EXIT
QEMU_EXTRA="-drive file=$ROOT,format=raw,if=ide,index=1,media=disk -drive file=$B/net-data.img,format=raw,if=ide,index=2,media=disk -nic user,model=${NIC_MODEL:-e1000},hostfwd=tcp:127.0.0.1:$P-:22" \
  tests/smoke/smoke.sh "$IMG" tests/smoke/expect/ssh.txt "$TIMEOUT" tests/smoke/keys/ssh.keys
status=0
wait $HOST || status=$?
cat "$B/ssh-host.log"
exit $status
