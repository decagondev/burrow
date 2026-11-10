#!/usr/bin/env bash
# census-net.sh - W24-S01: which system calls do the TLS clients make? Runs
# curl (build/net-root, from make net-ports) and busybox's wget on the host
# under strace -f, each fetching over TLS from tests/smoke/tls-server.py
# with a throwaway certificate for "localhost" (curl also refusing it for
# another name), and compares the set of names with
# tests/regression/census-net.txt, the way census-qt.sh does for Qt.
#
#   new name   -> FAIL: add it to census-net.txt with its Linux number, and
#                 either implement it in kernel/proc or name the slice that
#                 will (kernel/proc's TestCensusNet checks the file)
#   name gone  -> fine (reported, not a failure)
#
# Usage: tests/regression/census-net.sh [build-dir]
set -euo pipefail
B="${1:-build}"
NET="$B/net-root"
EXPECT="tests/regression/census-net.txt"
command -v strace >/dev/null || { echo "NET CENSUS FAIL: strace is not installed"; exit 1; }
[ -x "$NET/bin/curl" ] && [ -x "$B/busybox" ] || { echo "NET CENSUS SKIP (make net-ports busybox builds them)"; exit 0; }

W="$B/census-net-www"
rm -rf "$W" && mkdir -p "$W"
echo "fetched over TLS" > "$W/hello.txt"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=localhost -addext subjectAltName=DNS:localhost \
  -keyout "$B/census-net.key" -out "$B/census-net.pem" 2>/dev/null
PORT=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
python3 tests/smoke/tls-server.py "$PORT" "$W" "$B/census-net.pem" "$B/census-net.key" > "$B/census-net-server.log" 2>&1 &
SERVER=$!
trap 'kill $SERVER 2>/dev/null || true' EXIT
for _ in $(seq 100); do python3 -c "import socket; socket.create_connection(('127.0.0.1', $PORT))" 2>/dev/null && break; sleep 0.1; done

run() { # label, then the command
  local label=$1
  shift
  strace -f -qq -o "$B/census-net-$label.strace" "$@" > "$B/census-net-$label.out" 2>&1 ||
    { echo "NET CENSUS FAIL: $label exited with an error:"; cat "$B/census-net-$label.out"; exit 1; }
}
run curl "$NET/bin/curl" -sS --cacert "$B/census-net.pem" "https://localhost:$PORT/hello.txt"
grep -q "fetched over TLS" "$B/census-net-curl.out" || { echo "NET CENSUS FAIL: curl fetched something else"; exit 1; }
if strace -f -qq -o "$B/census-net-refused.strace" "$NET/bin/curl" -sS --cacert "$B/census-net.pem" \
  "https://127.0.0.1:$PORT/hello.txt" > "$B/census-net-refused.out" 2>&1; then
  echo "NET CENSUS FAIL: curl took a certificate for another name"; exit 1
fi
run wget "$B/busybox" wget -q -O - "https://localhost:$PORT/hello.txt"
grep -q "fetched over TLS" "$B/census-net-wget.out" || { echo "NET CENSUS FAIL: wget fetched something else"; exit 1; }

sed -nE 's/^[0-9]+ +([a-z_][a-z0-9_]*)\(.*/\1/p' "$B"/census-net-*.strace | grep -vx execve | sort -u > "$B/census-net.seen"
grep -v '^#' "$EXPECT" | awk 'NF {print $1}' | sort -u > "$B/census-net.expected"
new=$(comm -23 "$B/census-net.seen" "$B/census-net.expected")
gone=$(comm -13 "$B/census-net.seen" "$B/census-net.expected")
if [ -n "$gone" ]; then echo "net census: no longer used: $(echo $gone)"; fi
if [ -n "$new" ]; then
  echo "NET CENSUS FAIL: new syscalls: $(echo $new)"
  echo "  add them to $EXPECT with their numbers (and implement them or name the slice)"
  exit 1
fi
echo "NET CENSUS PASS ($(wc -l < "$B/census-net.seen") syscalls; kernel/proc's TestCensusNet checks each is served)"
