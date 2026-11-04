#!/usr/bin/env bash
# x11-linux.sh - the X programs on the host first (W16-S01): the static
# musl builds in build/x11-root run against the host's Xvfb, driven with
# xdotool, as they will run against Xfbdev on Burrow. xhello draws and
# takes a key; st runs busybox sh and its output reaches a file; dmenu
# returns the line chosen with the keyboard; dwm manages a window and quits
# on Mod+Shift+q. With "census" it runs each under strace -f and compares
# the set of system calls with tests/regression/census-x11.txt (new names
# fail; kernel/proc's TestCensusX11 checks each listed call is served).
# Xfbdev cannot run here (no fbdev on WSL): Burrow's unimplemented-syscall
# log is its census (W16-S02).
#
# Usage: tests/regression/x11-linux.sh [build-dir] [census]
set -euo pipefail
B="${1:-build}"
X="$B/x11-root"
CENSUS="${2:-}"
EXPECT=tests/regression/census-x11.txt
command -v Xvfb >/dev/null && command -v xdotool >/dev/null || { echo "X11 LINUX FAIL: needs xvfb and xdotool"; exit 1; }
[ -x "$X/bin/xhello" ] || { echo "X11 LINUX SKIP (make x11 builds the X stack)"; exit 0; }
[ -x "$B/busybox" ] || { echo "X11 LINUX FAIL: $B/busybox is missing (make busybox)"; exit 1; }

export DISPLAY=:97
export FONTCONFIG_FILE="$(cd "$B" && pwd)/x11-fonts.conf"
mkdir -p "$B/x11-run"
sed "s|/opt/x11/fonts/ttf|$(cd "$X/fonts/ttf" && pwd)|; s|/tmp/fontconfig|$(cd "$B" && pwd)/x11-run/fc-cache|" \
  "$X/etc/fonts/fonts.conf" > "$FONTCONFIG_FILE"
Xvfb "$DISPLAY" -screen 0 1024x768x24 -nolisten tcp > "$B/x11-run/xvfb.log" 2>&1 &
XVFB=$!
trap 'kill $XVFB 2>/dev/null || true' EXIT
for _ in $(seq 50); do xdotool getdisplaygeometry > /dev/null 2>&1 && break; sleep 0.1; done

STEP_S=60 # no step may take longer: a client that never sees its keys fails, not hangs
run() { # label, then the command: traced when taking the census
  local label=$1
  shift
  if [ -n "$CENSUS" ]; then
    timeout --foreground "$STEP_S" strace -f -qq -o "$B/x11-run/$label.strace" "$@"
  else
    timeout --foreground "$STEP_S" "$@"
  fi
}
fail() { echo "X11 LINUX FAIL: $*"; exit 1; }

# xhello: draws, then q quits it.
run xhello "$X/bin/xhello" > "$B/x11-run/xhello.out" 2>&1 &
H=$!
for _ in $(seq 50); do grep -q exposed "$B/x11-run/xhello.out" && break; sleep 0.1; done
xdotool search --sync --name xhello windowfocus --sync key q 2>/dev/null || true # the release may find the window gone
wait $H || fail "xhello exited with an error: $(cat "$B/x11-run/xhello.out")"
grep -q "Xft font DejaVu Sans" "$B/x11-run/xhello.out" && grep -q "xhello: key q" "$B/x11-run/xhello.out" ||
  fail "xhello: $(cat "$B/x11-run/xhello.out")"

# st: busybox sh in a terminal writes a file.
rm -f "$B/x11-run/st.ok"
# st's exit status when its shell ends is a race in st itself (it may die on
# the pty's EIO before its SIGCHLD handler exits 0): judge it by its work.
run st "$X/bin/st" -e "$B/busybox" sh -c "echo st-ran-a-shell > $B/x11-run/st.ok" > "$B/x11-run/st.out" 2>&1 || true
grep -q st-ran-a-shell "$B/x11-run/st.ok" 2>/dev/null || fail "st's shell left nothing: $(cat "$B/x11-run/st.out")"

# dmenu: three lines, Down, Return: the second comes back. The keys wait for
# dmenu's window (it grabs the keyboard before mapping it): a fixed sleep lost
# them once Xlib's locale data made dmenu start slower under strace (W16-S04).
(timeout "$STEP_S" xdotool search --sync --class dmenu > /dev/null && sleep 0.3; xdotool key Down Return) &
picked=$(printf 'one\ntwo\nthree\n' | run dmenu "$X/bin/dmenu")
[ "$picked" = two ] || fail "dmenu picked '$picked'"

# dwm: manages xhello's window, then Alt+Shift+q quits it.
run dwm "$X/bin/dwm" > "$B/x11-run/dwm.out" 2>&1 &
W=$!
sleep 1
"$X/bin/xhello" --quit-after 1500 > /dev/null 2>&1
xdotool key alt+shift+q
wait $W || fail "dwm: $(cat "$B/x11-run/dwm.out")"
echo "X11 LINUX PASS (xhello, st, dmenu, dwm against Xvfb)"

[ -n "$CENSUS" ] || exit 0
sed -nE 's/^[0-9]+ +([a-z_][a-z0-9_]*)\(.*/\1/p' "$B"/x11-run/*.strace | grep -vx execve | sort -u > "$B/census-x11.seen"
grep -v '^#' "$EXPECT" | grep -v '# vdso' | awk 'NF {print $1}' | sort -u > "$B/census-x11.expected"
new=$(comm -23 "$B/census-x11.seen" "$B/census-x11.expected")
gone=$(comm -13 "$B/census-x11.seen" "$B/census-x11.expected")
[ -n "$gone" ] && echo "x11 census: no longer used: $(echo $gone)"
[ -z "$new" ] || { echo "X11 CENSUS FAIL: new syscalls: $(echo $new)"; exit 1; }
echo "X11 CENSUS PASS ($(wc -l < "$B/census-x11.seen") syscalls)"
