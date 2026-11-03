#!/usr/bin/env bash
# doom.sh - fetch, verify and build Doom for Burrow (W11-S03/S04).
# Downloads doomgeneric at a pinned commit and the Freedoom release once
# into $DOOM_CACHE (default ~/.cache/burrow-doom), refuses either unless its
# sha256 matches the pin (Freedoom's is the one in its signed CHECKSUM file),
# compiles doomgeneric's engine with musl-gcc and the Burrow backend
# (user/doom/doomgeneric_burrow.c, Linux fbdev + evdev only) into
# <out-dir>/doom, and extracts freedoom1.wad there. Never builds inside the
# unpacked source and runs nothing from it. Docs:
# docs/04-plan/waves/WAVE-11-doom.md.
#
# Usage: scripts/doom.sh <out-dir> <uapi-include-dir>   (MUSL_GCC=... overrides)
set -euo pipefail
OUT="${1:?usage: doom.sh <out-dir> <uapi-include-dir>}"
UAPI="${2:?usage: doom.sh <out-dir> <uapi-include-dir>}"
DG_COMMIT=e257a09527062bfb986b6e5e0780839b6cbd5b71
DG_SHA256=26f8f60daa6c6e4d3c01e5eac9fb700d592dede803d13ac848154ff356b2a2e7
DG_URL="https://codeload.github.com/ozkl/doomgeneric/tar.gz/$DG_COMMIT"
FD_VERSION=0.13.0
FD_SHA256=3f9b264f3e3ce503b4fb7f6bdcb1f419d93c7b546f4df3e874dd878db9688f59
FD_URL="https://github.com/freedoom/freedoom/releases/download/v$FD_VERSION/freedoom-$FD_VERSION.zip"
CACHE="${DOOM_CACHE:-$HOME/.cache/burrow-doom}"
MUSL_GCC="${MUSL_GCC:-musl-gcc}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BACKEND="$ROOT/user/doom/doomgeneric_burrow.c"

# doomgeneric's engine: its Makefile's SRC_DOOM without the X11 backend.
ENGINE="dummy am_map doomdef doomstat dstrings d_event d_items d_iwad d_loop d_main d_mode d_net
 f_finale f_wipe g_game hu_lib hu_stuff info i_cdmus i_endoom i_joystick i_scale i_sound i_system
 i_timer memio m_argv m_bbox m_cheat m_config m_controls m_fixed m_menu m_misc m_random p_ceilng
 p_doors p_enemy p_floor p_inter p_lights p_map p_maputl p_mobj p_plats p_pspr p_saveg p_setup
 p_sight p_spec p_switch p_telept p_tick p_user r_bsp r_data r_draw r_main r_plane r_segs r_sky
 r_things sha1 sounds statdump st_lib st_stuff s_sound tables v_video wi_stuff w_checksum w_file
 w_main w_wad z_zone w_file_stdc i_input i_video doomgeneric"

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
"$MUSL_GCC" -static -o "$OUT/doom" "${objs[@]}" "$BUILD/doomgeneric_burrow.o" -lm
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
