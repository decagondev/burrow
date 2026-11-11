#!/usr/bin/env bash
# desktop-ssh.sh - desktop-smoke's first boot with the host's ssh (W24-S06):
# makes a throwaway key (on a Linux file system: OpenSSH refuses a private
# key /mnt/c shows as everyone's), serves its public half from the host
# (python3 http.server), puts that port in place of @PORT@ in the keys file,
# starts tests/smoke/desktop-ssh.py against a free host port forwarded to
# Burrow's 22, and boots go-dev.img with the caller's QEMU_EXTRA (the disks)
# plus QEMU's user network. The keys file starts dropbear before startx.
#
# Usage: tests/smoke/desktop-ssh.sh <build-dir> <expect> <keys> <timeout>
set -euo pipefail
B="$1" EXPECT="$2" KEYS="$3" TIMEOUT="$4"
WORK="${NET_CACHE:-$HOME/.cache/burrow-net}/desktop-ssh"
WWW="$B/desktop-www"
mkdir -p "$WORK" && rm -f "$WORK/key" "$WORK/key.pub" "$WORK/known_hosts"
ssh-keygen -q -t ed25519 -N "" -f "$WORK/key"
rm -rf "$WWW" && mkdir -p "$WWW" && cp "$WORK/key.pub" "$WWW/key.pub"
port() { python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'; }
PORT=$(port) SSHPORT=$(port)
python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$WWW" > "$B/desktop-www.log" 2>&1 &
SERVER=$!
python3 tests/smoke/desktop-ssh.py "$SSHPORT" "$WORK" > "$B/desktop-ssh.log" 2>&1 &
HOST=$!
trap 'kill $SERVER $HOST 2>/dev/null || true' EXIT
sed "s/@PORT@/$PORT/g" "$KEYS" > "$B/desktop-1.keys"
QEMU_EXTRA="${QEMU_EXTRA:-} -nic user,model=${NIC_MODEL:-e1000},hostfwd=tcp:127.0.0.1:$SSHPORT-:22" \
  tests/smoke/smoke.sh "$B/go-dev.img" "$EXPECT" "$TIMEOUT" "$B/desktop-1.keys"
status=0
wait $HOST || status=$?
cat "$B/desktop-ssh.log"
exit $status
