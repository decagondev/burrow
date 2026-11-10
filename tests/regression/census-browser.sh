#!/usr/bin/env bash
# census-browser.sh - W24-S04: which system calls does NetSurf make? Runs
# build/net-root/bin/netsurf (scripts/browser.sh) on the host's Xvfb under
# strace -f, loading the test pages (tests/smoke/browser-pages.py) from a
# python3 http.server; once the page's image has been fetched it saves the
# screen, checks the styled box and the image are drawn, and quits NetSurf
# with its window's close button. The set of names is compared with
# tests/regression/census-browser.txt, as census-net.sh does:
#
#   new name   -> FAIL: add it to census-browser.txt with its Linux number,
#                 and either implement it in kernel/proc or name the slice
#                 that will (kernel/proc's TestCensusBrowser checks the file)
#   name gone  -> fine (reported, not a failure)
#
# Usage: tests/regression/census-browser.sh [build-dir]
set -euo pipefail
export LC_ALL=C
B="${1:-build}"
NS="$B/net-root"
EXPECT="tests/regression/census-browser.txt"
for t in strace Xvfb xdotool xwd; do
  command -v $t > /dev/null || { echo "BROWSER CENSUS FAIL: $t is not installed"; exit 1; }
done
[ -x "$NS/bin/netsurf" ] || { echo "BROWSER CENSUS SKIP (make browser builds NetSurf)"; exit 0; }

R="$B/census-browser"
rm -rf "$R" && mkdir -p "$R/home"
python3 tests/smoke/browser-pages.py "$R/www"
PORT=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$R/www" > "$R/server.log" 2>&1 &
SERVER=$!
export DISPLAY=:95
Xvfb "$DISPLAY" -screen 0 1024x768x24 -nolisten tcp > "$R/xvfb.log" 2>&1 &
XVFB=$!
trap 'kill $SERVER $XVFB 2>/dev/null || true' EXIT
for _ in $(seq 50); do xdotool getdisplaygeometry > /dev/null 2>&1 && break; sleep 0.1; done

NETSURFRES="$(cd "$NS/share/netsurf" && pwd)" HOME="$R/home" timeout 120 strace -f -qq -o "$R/netsurf.strace" \
  "$NS/bin/netsurf" -f x -w 800 -h 600 "http://127.0.0.1:$PORT/" > "$R/netsurf.out" 2>&1 &
NETSURF=$!
for _ in $(seq 300); do grep -q 'GET /square.png' "$R/server.log" && break; sleep 0.2; done
grep -q 'GET /square.png' "$R/server.log" || { echo "BROWSER CENSUS FAIL: NetSurf never fetched the image"; cat "$R/netsurf.out"; exit 1; }
sleep 3
xwd -root -silent > "$R/screen.xwd"
python3 tests/regression/xwdpixel.py "$R/screen.xwd" 140 200 20a040 48 280 d03030 ||
  { echo "BROWSER CENSUS FAIL: the page is not drawn"; exit 1; }
xdotool mousemove 786 15 click 1 # the close button at the toolbar's right end
wait $NETSURF || { echo "BROWSER CENSUS FAIL: NetSurf exited with an error:"; cat "$R/netsurf.out"; exit 1; }

sed -nE 's/^[0-9]+ +([a-z_][a-z0-9_]*)\(.*/\1/p' "$R/netsurf.strace" | grep -vx execve | sort -u > "$B/census-browser.seen"
grep -v '^#' "$EXPECT" | awk 'NF {print $1}' | sort -u > "$B/census-browser.expected"
new=$(comm -23 "$B/census-browser.seen" "$B/census-browser.expected")
gone=$(comm -13 "$B/census-browser.seen" "$B/census-browser.expected")
if [ -n "$gone" ]; then echo "browser census: no longer used: $(echo $gone)"; fi
if [ -n "$new" ]; then
  echo "BROWSER CENSUS FAIL: new syscalls: $(echo $new)"
  echo "  add them to $EXPECT with their numbers (and implement them or name the slice)"
  exit 1
fi
echo "BROWSER CENSUS PASS ($(wc -l < "$B/census-browser.seen") syscalls; kernel/proc's TestCensusBrowser checks each is served)"
