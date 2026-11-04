#!/usr/bin/env bash
# x11-doom.sh - Doom in an X window (W16-S05): doomgeneric's engine, as
# unpacked by `make doom` (scripts/doom.sh, which downloads and checks it),
# compiled with the musl cross compiler `make x11` uses, linked with
# user/x11/doom/doomgeneric_x11.c and the static X libraries into
# <x11-root>/bin/doom, with the WAD copied to <x11-root>/share/doom (which
# doom uses when started without -iwad) and the fbdev Doom beside it as
# doom-fbdev for make x11-bench (user/x11/xbench.c). Never downloads, and never builds inside the unpacked source:
# objects go to the X cache. Docs: docs/04-plan/waves/WAVE-16-x11-desktop.md.
#
# Usage: scripts/x11-doom.sh <x11-root> <wad>
set -euo pipefail
OUT="${1:?usage: x11-doom.sh <x11-root> <wad>}"
WAD="${2:?usage: x11-doom.sh <x11-root> <wad>}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "$ROOT/scripts/doom-engine.sh" # DG_COMMIT, ENGINE
SRC="${DOOM_CACHE:-$HOME/.cache/burrow-doom}/src/doomgeneric-$DG_COMMIT/doomgeneric"
CACHE="${X11_CACHE:-$HOME/.cache/burrow-x11}"
SYS="$CACHE/sysroot"
CC="${PORTS_CACHE:-$HOME/.cache/burrow-ports}/cross/x86_64-linux-musl-cross/bin/x86_64-linux-musl-gcc"
BACKEND="$ROOT/user/x11/doom/doomgeneric_x11.c"

[ -d "$SRC" ] || { echo "x11-doom: no doomgeneric source in $SRC (make doom)"; exit 1; }
[ -x "$CC" ] && [ -f "$SYS/lib/libX11.a" ] || { echo "x11-doom: needs make ports x11"; exit 1; }

# Each engine file once (objects cached per commit), the backend every time.
BUILD="$CACHE/build/doom-$DG_COMMIT"
mkdir -p "$BUILD" "$OUT/bin" "$OUT/share/doom"
CFLAGS="-O2 -DNORMALUNIX -DLINUX -D_DEFAULT_SOURCE -I$SRC -w"
objs=()
for f in $ENGINE; do
  o="$BUILD/$f.o"
  [ -f "$o" ] || "$CC" $CFLAGS -c "$SRC/$f.c" -o "$o"
  objs+=("$o")
done
"$CC" $CFLAGS -I"$SYS/include" -DDOOM_WAD="\"/opt/x11/share/doom/$(basename "$WAD")\"" \
  -c "$BACKEND" -o "$BUILD/doomgeneric_x11.o"
"$CC" -static -o "$OUT/bin/doom" "${objs[@]}" "$BUILD/doomgeneric_x11.o" \
  -L"$SYS/lib" -lXext -lX11 -lxcb -lXau -lXdmcp -lm
"$(dirname "$CC")/x86_64-linux-musl-strip" "$OUT/bin/doom"
cp "$WAD" "$OUT/share/doom/"
# The fbdev Doom (scripts/doom.sh) beside it: make x11-bench compares the two.
cp "$(dirname "$WAD")/doom" "$OUT/share/doom/doom-fbdev"
echo "x11-doom: doomgeneric ${DG_COMMIT:0:7} -> $OUT/bin/doom ($(stat -c %s "$OUT/bin/doom") bytes), $(basename "$WAD")"
