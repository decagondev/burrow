#!/usr/bin/env bash
# doom.sh - fetch, verify and build Doom for Burrow (W11-S03/S04).
# Downloads doomgeneric at a pinned commit and the Freedoom release once
# into $DOOM_CACHE (default ~/.cache/burrow-doom), refuses either unless its
# sha256 matches the pin (Freedoom's is the one in its signed CHECKSUM file),
# compiles doomgeneric's engine with musl-gcc and the Burrow backend
# (user/doom/doomgeneric_burrow.c, Linux fbdev + evdev only; sound through
# /dev/dsp by user/doom/i_burrowsound.c since W22-S03) into
# <out-dir>/doom, and extracts freedoom1.wad there. Never builds inside the
# unpacked source and runs nothing from it. Docs:
# docs/04-plan/waves/WAVE-11-doom.md.
#
# Usage: scripts/doom.sh <out-dir> <uapi-include-dir>   (MUSL_GCC=... overrides)
set -euo pipefail
OUT="${1:?usage: doom.sh <out-dir> <uapi-include-dir>}"
UAPI="${2:?usage: doom.sh <out-dir> <uapi-include-dir>}"
source "$(dirname "$0")/doom-engine.sh" # DG_COMMIT, DG_SHA256, ENGINE
DG_URL="https://codeload.github.com/ozkl/doomgeneric/tar.gz/$DG_COMMIT"
FD_VERSION=0.13.0
FD_SHA256=3f9b264f3e3ce503b4fb7f6bdcb1f419d93c7b546f4df3e874dd878db9688f59
FD_URL="https://github.com/freedoom/freedoom/releases/download/v$FD_VERSION/freedoom-$FD_VERSION.zip"
CACHE="${DOOM_CACHE:-$HOME/.cache/burrow-doom}"
MUSL_GCC="${MUSL_GCC:-musl-gcc}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BACKEND="$ROOT/user/doom/doomgeneric_burrow.c"

command -v "$MUSL_GCC" >/dev/null || { echo "doom: $MUSL_GCC not found (install musl-tools or set MUSL_GCC)"; exit 1; }

fetch() { # url file sha256
  if [ ! -f "$2" ]; then
    mkdir -p "$(dirname "$2")"
    echo "doom: downloading $1"
    curl -fsSL -o "$2.part" "$1"
    mv "$2.part" "$2"
  fi
  echo "$3  $2" | sha256sum -c --quiet - || { echo "doom: $2 does not match the pinned sha256"; exit 1; }
}
DG_TAR="$CACHE/dl/doomgeneric-$DG_COMMIT.tar.gz"
FD_ZIP="$CACHE/dl/freedoom-$FD_VERSION.zip"
fetch "$DG_URL" "$DG_TAR" "$DG_SHA256"
fetch "$FD_URL" "$FD_ZIP" "$FD_SHA256"

SRC="$CACHE/src/doomgeneric-$DG_COMMIT/doomgeneric"
if [ ! -d "$SRC" ]; then
  mkdir -p "$CACHE/src"
  tar -xzf "$DG_TAR" -C "$CACHE/src" --no-same-owner
fi

# Compile each engine file once (objects cached), the backend every time.
BUILD="$CACHE/build-$DG_COMMIT"
mkdir -p "$BUILD" "$OUT"
CFLAGS="-static -O2 -DNORMALUNIX -DLINUX -D_DEFAULT_SOURCE -isystem $UAPI -I$SRC -w"
objs=()
for f in $ENGINE; do
  o="$BUILD/$f.o"
  [ -f "$o" ] || "$MUSL_GCC" $CFLAGS -c "$SRC/$f.c" -o "$o"
  objs+=("$o")
done
"$MUSL_GCC" $CFLAGS -c "$BACKEND" -o "$BUILD/doomgeneric_burrow.o"
# W22-S03: i_sound with FEATURE_SOUND, and Burrow's module, every time.
sound_objs=()
for f in $SOUND_ENGINE; do
  "$MUSL_GCC" $CFLAGS $SOUND_FLAGS -I"$ROOT/$SOUND_STUB" -c "$SRC/$f.c" -o "$BUILD/${f}_burrow.o"
  sound_objs+=("$BUILD/${f}_burrow.o")
done
"$MUSL_GCC" $CFLAGS $SOUND_FLAGS -c "$ROOT/$SOUND_MODULE" -o "$BUILD/i_burrowsound.o"
"$MUSL_GCC" -static -o "$OUT/doom" "${objs[@]}" "$BUILD/doomgeneric_burrow.o" "${sound_objs[@]}" "$BUILD/i_burrowsound.o" -lm
echo "doom: doomgeneric ${DG_COMMIT:0:7} -> $OUT/doom ($(stat -c %s "$OUT/doom") bytes)"

# BUG-0004: the evdev -> Doom key map, compiled from the backend itself.
"$MUSL_GCC" $CFLAGS -I"$(dirname "$BACKEND")" "$ROOT/tests/unit/doom/keymap_test.c" -o "$BUILD/keymap_test"
"$BUILD/keymap_test"

python3 -I -c '
import sys, zipfile
z = zipfile.ZipFile(sys.argv[1])
with z.open(sys.argv[2]) as src, open(sys.argv[3], "wb") as dst:
    dst.write(src.read())
' "$FD_ZIP" "freedoom-$FD_VERSION/freedoom1.wad" "$OUT/freedoom1.wad"
echo "doom: Freedoom $FD_VERSION -> $OUT/freedoom1.wad ($(stat -c %s "$OUT/freedoom1.wad") bytes)"
