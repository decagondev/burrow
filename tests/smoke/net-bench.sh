#!/usr/bin/env bash
# net-bench.sh - how fast Burrow's network is (W17-S06, make net-bench, not
# in make test): serves 10 MB of random bytes from the host (python3
# http.server, a free loopback port) for gofetch on Burrow to download, and
# forwards a host port to busybox httpd in Burrow for tests/smoke/fetch.py
# to time 200 short connections; then tests/smoke/net-budgets.py checks the
# numbers. Needs nothing beyond the host.
#
# Usage: tests/smoke/net-bench.sh <build-dir> <img> [timeout]
set -euo pipefail
B="$1" IMG="$2" TIMEOUT="${3:-240}"
free() { python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'; }
WWW="$B/net-www"
mkdir -p "$WWW"
python3 -c 'import random, sys; random.seed(10); sys.stdout.buffer.write(random.randbytes(10 << 20))' > "$WWW/ten.bin"
PORT=$(free) FWD=$(free)
python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$WWW" > "$B/net-www.log" 2>&1 &
SERVER=$!
python3 tests/smoke/fetch.py bench "$FWD" initrd/etc motd 200 > "$B/net-bench-fetch.log" 2>&1 &
FETCH=$!
trap 'kill $SERVER $FETCH 2>/dev/null || true' EXIT
sed "s/@PORT@/$PORT/g" tests/smoke/keys/net-bench.keys > "$B/net-bench.keys"
QEMU_EXTRA="-nic user,model=e1000,hostfwd=tcp:127.0.0.1:$FWD-:8080" \
  tests/smoke/smoke.sh "$IMG" tests/smoke/expect/net-bench.txt "$TIMEOUT" "$B/net-bench.keys"
wait $FETCH
cat "$B/net-bench-fetch.log"
python3 tests/smoke/net-budgets.py "$B/smoke-net-bench.log" "$B/net-bench-fetch.log"
