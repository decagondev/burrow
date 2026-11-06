#!/usr/bin/env bash
# net-smoke.sh - Burrow on the network (W17-S04): starts Python's HTTP
# server on the host (on a free port, loopback only) over a directory with a
# text file and a 1 MiB file, writes the port into the keys file, and boots
# <img> on QEMU's user network, where the host is 10.0.2.2 ("host" in
# Burrow's /etc/hosts). Burrow's busybox fetches both files and pings the
# gateway; the expect file checks what it printed, including the 1 MiB
# file's size and checksum. W17-S06: a second, HTTPS server (a self-signed
# certificate made here with openssl) for gofetch's TLS handshake. Needs
# nothing beyond the host: no internet.
#
# Usage: tests/smoke/net-smoke.sh <build-dir> <img> <name> [timeout]
set -euo pipefail
B="$1" IMG="$2" NAME="$3" TIMEOUT="${4:-120}"
WWW="$B/net-www"
rm -rf "$WWW" && mkdir -p "$WWW"
echo "burrow fetched this from the host" > "$WWW/hello.txt"
python3 -c 'import random, sys; random.seed(17); sys.stdout.buffer.write(random.randbytes(1 << 20))' > "$WWW/big.bin"
PORT=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$WWW" > "$B/net-www.log" 2>&1 &
SERVER=$!
TLSPORT=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=host -addext subjectAltName=DNS:host   -keyout "$B/net-tls.key" -out "$B/net-tls.pem" 2>/dev/null
python3 tests/smoke/tls-server.py "$TLSPORT" "$WWW" "$B/net-tls.pem" "$B/net-tls.key" > "$B/net-tls.log" 2>&1 &
TLSSERVER=$!
trap 'kill $SERVER $TLSSERVER 2>/dev/null || true' EXIT
for p in $PORT $TLSPORT; do # both servers listen before Burrow boots (the first TLS fetch once came too early)
  for _ in $(seq 100); do python3 -c "import socket; socket.create_connection(('127.0.0.1', $p))" 2>/dev/null && break; sleep 0.1; done
done
sed "s/@PORT@/$PORT/g; s/@TLSPORT@/$TLSPORT/g; s/@SUM@/$(md5sum < "$WWW/big.bin" | cut -d' ' -f1)/g" "tests/smoke/keys/$NAME.keys" > "$B/$NAME.keys"
# W23-S05: the card is NIC_MODEL's (QEMU's default e1000 unless set)
QEMU_EXTRA="${QEMU_EXTRA:-} -nic user,model=${NIC_MODEL:-e1000}" \
  tests/smoke/smoke.sh "$IMG" "tests/smoke/expect/$NAME.txt" "$TIMEOUT" "$B/$NAME.keys"
