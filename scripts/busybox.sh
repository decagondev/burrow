#!/usr/bin/env bash
# busybox.sh - fetch, verify and build busybox for Burrow (W10-S08).
# Downloads the pinned source release once into $BUSYBOX_CACHE (default
# ~/.cache/burrow-busybox), refuses it unless its sha256 matches the pin,
# configures it from user/busybox.config on top of allnoconfig, builds it
# static with musl-gcc out of tree, and copies the binary to <out>.
# Never builds inside the unpacked source and never runs anything from it
# but its own Makefile. Docs: docs/04-plan/waves/WAVE-10-a-unix.md (S08).
#
# Usage: scripts/busybox.sh <out>      (MUSL_GCC=... overrides musl-gcc)
set -euo pipefail
OUT="${1:?usage: busybox.sh <out>}"
VERSION=1.36.1                       # the newest release busybox.net marks stable
SHA256=b8cc24c9574d809e7279c3be349795c5d5ceb6fdf19ca709f80cde50e47de314
URL="https://busybox.net/downloads/busybox-$VERSION.tar.bz2"
CACHE="${BUSYBOX_CACHE:-$HOME/.cache/burrow-busybox}"
MUSL_GCC="${MUSL_GCC:-musl-gcc}"
CONFIG="$(cd "$(dirname "$0")/.." && pwd)/user/busybox.config"

command -v "$MUSL_GCC" >/dev/null || { echo "busybox: $MUSL_GCC not found (install musl-tools or set MUSL_GCC)"; exit 1; }
TARBALL="$CACHE/dl/busybox-$VERSION.tar.bz2"
SRC="$CACHE/src/busybox-$VERSION"
BUILD="$CACHE/build-$VERSION"

if [ ! -f "$TARBALL" ]; then
  mkdir -p "$CACHE/dl"
  echo "busybox: downloading $URL"
  curl -fsSL -o "$TARBALL.part" "$URL"
  mv "$TARBALL.part" "$TARBALL"
fi
echo "$SHA256  $TARBALL" | sha256sum -c --quiet - || { echo "busybox: $TARBALL does not match the pinned sha256"; exit 1; }
if [ ! -d "$SRC" ]; then
  mkdir -p "$CACHE/src"
  tar -xjf "$TARBALL" -C "$CACHE/src" --no-same-owner
fi

# Configure: allnoconfig, then every option in busybox.config, then let
# oldconfig settle the dependents. Rebuilt whenever busybox.config changes.
mkdir -p "$BUILD"
if [ ! -f "$BUILD/.config" ] || [ "$CONFIG" -nt "$BUILD/.config" ]; then
  make -s -C "$SRC" O="$BUILD" allnoconfig >/dev/null
  while read -r line; do
    line="${line%%#*}"; line="${line// /}"
    [ -n "$line" ] || continue
    key="${line%%=*}"; value=y
    [ "$key" = "$line" ] || value="${line#*=}"
    sed -i "/^CONFIG_$key=/d; /^# CONFIG_$key is not set/d" "$BUILD/.config"
    echo "CONFIG_$key=$value" >> "$BUILD/.config"
  done < "$CONFIG"
  yes "" | make -s -C "$BUILD" oldconfig >/dev/null 2>&1 || true
fi
make -s -C "$BUILD" -j"$(nproc)" CC="$MUSL_GCC" HOSTCC=gcc >/dev/null
cp "$BUILD/busybox" "$OUT"
echo "busybox: $VERSION -> $OUT ($(stat -c %s "$OUT") bytes)"
