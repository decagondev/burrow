#!/usr/bin/env bash
# net-serve.sh - servers inside Burrow (W17-S05): boots <img> with a fresh
# home disk and two ports forwarded from the host (QEMU's hostfwd, on free
# loopback ports) to busybox httpd (8080) and goserve (8081) in Burrow,
# runs tests/smoke/fetch.py against them while the keys file starts,
# restarts and stops the servers, and fails unless both sides pass.
#
# Usage: tests/smoke/net-serve.sh <build-dir> <img> <home-img> [timeout]
set -euo pipefail
B="$1" IMG="$2" HOME_DISK="$3" TIMEOUT="${4:-240}"
free() { python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'; }
P1=$(free) P2=$(free)
python3 tests/smoke/fetch.py "$P1" "$P2" workbench hello.c 50 > "$B/net-serve-fetch.log" 2>&1 &
FETCH=$!
trap 'kill $FETCH 2>/dev/null || true' EXIT
NIC="-nic user,model=${NIC_MODEL:-e1000},hostfwd=tcp:127.0.0.1:$P1-:8080,hostfwd=tcp:127.0.0.1:$P2-:8081"
QEMU_EXTRA="-drive file=$HOME_DISK,format=raw,if=ide,index=1,media=disk $NIC" \
  tests/smoke/smoke.sh "$IMG" tests/smoke/expect/net-serve.txt "$TIMEOUT" tests/smoke/keys/net-serve.keys
status=0
wait $FETCH || status=$?
cat "$B/net-serve-fetch.log"
[ "$status" -eq 0 ] || { echo "NET SERVE FAIL: the host's fetches failed"; exit 1; }
echo "NET SERVE PASS (the host fetched from httpd, a restarted httpd and goserve)"
