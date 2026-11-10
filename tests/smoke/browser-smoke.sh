#!/usr/bin/env bash
# browser-smoke.sh - NetSurf on Burrow (W24-S04, S05; make browser-smoke):
# writes the test pages (tests/smoke/browser-pages.py) and serves them from
# the host: python3's http.server on a free loopback port (10.0.2.2, "host"
# in Burrow's /etc/hosts), and since S05 two TLS servers (tls-server.py) on
# the same pages, one with a certificate for "host" (made here, served as
# ca.pem for NetSurf's --ca_bundle) and one whose certificate names another
# host and is signed by nothing NetSurf trusts. The ports go into the keys
# file, and <img> (go-dev.img) boots with the X disk as hdb, the NETDATA
# disk as hdc, QEMU's user network and its tablet. Needs nothing beyond the
# host: no internet.
#
# Usage: tests/smoke/browser-smoke.sh <build-dir> <img> <name> [timeout]
set -euo pipefail
B="$1" IMG="$2" NAME="$3" TIMEOUT="${4:-300}"
WWW="$B/browser-www"
rm -rf "$WWW"
python3 tests/smoke/browser-pages.py "$WWW"
port() { python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'; }
PORT=$(port) TLSPORT=$(port) BADPORT=$(port)
cert() { # name file-stem
  openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=$1" -addext "subjectAltName=DNS:$1" \
    -keyout "$2.key" -out "$2.pem" 2> /dev/null
}
cert host "$B/browser-tls"
cert elsewhere "$B/browser-bad"
cp "$B/browser-tls.pem" "$WWW/ca.pem"
python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$WWW" > "$B/browser-www.log" 2>&1 &
PIDS=$!
python3 tests/smoke/tls-server.py "$TLSPORT" "$WWW" "$B/browser-tls.pem" "$B/browser-tls.key" > "$B/browser-tls.log" 2>&1 &
PIDS="$PIDS $!"
python3 tests/smoke/tls-server.py "$BADPORT" "$WWW" "$B/browser-bad.pem" "$B/browser-bad.key" > "$B/browser-bad.log" 2>&1 &
PIDS="$PIDS $!"
trap 'kill $PIDS 2>/dev/null || true' EXIT
for p in $PORT $TLSPORT $BADPORT; do
  for _ in $(seq 100); do python3 -c "import socket; socket.create_connection(('127.0.0.1', $p))" 2>/dev/null && break; sleep 0.1; done
done
sed "s/@PORT@/$PORT/g; s/@TLSPORT@/$TLSPORT/g; s/@BADPORT@/$BADPORT/g" "tests/smoke/keys/$NAME.keys" > "$B/$NAME.keys"
QEMU_EXTRA="-drive file=$B/x11-data.img,format=raw,if=ide,index=1,media=disk -drive file=$B/net-data.img,format=raw,if=ide,index=2,media=disk -nic user,model=${NIC_MODEL:-e1000} -device virtio-tablet-pci" \
  tests/smoke/smoke.sh "$IMG" "tests/smoke/expect/$NAME.txt" "$TIMEOUT" "$B/$NAME.keys"
echo "browser: the host served $(grep -c 'GET ' "$B/browser-www.log") requests over HTTP," \
  "$(grep -c . "$B/browser-tls.log") lines of TLS log, $(grep -c . "$B/browser-bad.log") on the bad server"
