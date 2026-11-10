#!/usr/bin/env bash
# browser-smoke.sh - NetSurf on Burrow (W24-S04, make browser-smoke): writes
# the test pages (tests/smoke/browser-pages.py), serves them from the host
# with python3's http.server on a free loopback port (10.0.2.2, "host" in
# Burrow's /etc/hosts), puts the port into the keys file and boots <img>
# (go-dev.img) with the X disk as hdb, the NETDATA disk as hdc and QEMU's
# user network. The keys start Xfbdev and NetSurf on the page; the screen
# file checks that the styled box and the image are drawn. Needs nothing
# beyond the host: no internet.
#
# Usage: tests/smoke/browser-smoke.sh <build-dir> <img> <name> [timeout]
set -euo pipefail
B="$1" IMG="$2" NAME="$3" TIMEOUT="${4:-300}"
WWW="$B/browser-www"
rm -rf "$WWW"
python3 tests/smoke/browser-pages.py "$WWW"
PORT=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$WWW" > "$B/browser-www.log" 2>&1 &
SERVER=$!
trap 'kill $SERVER 2>/dev/null || true' EXIT
for _ in $(seq 100); do python3 -c "import socket; socket.create_connection(('127.0.0.1', $PORT))" 2>/dev/null && break; sleep 0.1; done
sed "s/@PORT@/$PORT/g" "tests/smoke/keys/$NAME.keys" > "$B/$NAME.keys"
QEMU_EXTRA="-drive file=$B/x11-data.img,format=raw,if=ide,index=1,media=disk -drive file=$B/net-data.img,format=raw,if=ide,index=2,media=disk -nic user,model=${NIC_MODEL:-e1000}" \
  tests/smoke/smoke.sh "$IMG" "tests/smoke/expect/$NAME.txt" "$TIMEOUT" "$B/$NAME.keys"
echo "browser: the host served: $(grep -c 'GET ' "$B/browser-www.log") requests"
