#!/usr/bin/env bash
# qt.sh - fetch, verify and build Qt for Burrow (W15-S01): qtbase 6.12.0
# (Core, Gui, Widgets) static against musl, drawn by the linuxfb platform
# with Qt's bundled zlib, libpng, FreeType, HarfBuzz and PCRE2, plus the
# DejaVu fonts. Qt cross-compiles against a host build of the same version
# (QT_HOST_PATH: moc, rcc, uic), so qtbase is built twice: for the host
# (tools, shared, quick to make) and for Burrow (static, musl.cc GCC 11.2.1
# from scripts/ports.sh). Downloads go once into $QT_CACHE (default
# ~/.cache/burrow-qt) and are refused unless their sha256 matches the pin
# (qtbase's and the security diff's are download.qt.io's; DejaVu publishes
# none, so its pin is the one distributions use for 2.37). Never run inside
# make test: the first build takes a long while. qtbase is LGPLv3/GPL: the
# source download and this script are what static linking under the LGPL
# asks Burrow to ship. Docs: docs/04-plan/waves/WAVE-15-qt.md, PORTING.md.
#
# Usage: scripts/qt.sh <out-dir>    (out: bin/, fonts/, the tree for /opt/qt)
#        QT_LIBS_ONLY=1 stops after building qtbase.
set -euo pipefail
OUT="$(mkdir -p "${1:?usage: qt.sh <out-dir>}" && cd "$1" && pwd)"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE="${QT_CACHE:-$HOME/.cache/burrow-qt}"
PORTS="${PORTS_CACHE:-$HOME/.cache/burrow-ports}"
JOBS="${JOBS:-$(nproc)}"
QT_VERSION=6.12.0
QT_SHA256=a951bd163c7b80fc6b8c88d7668fb56abf91c152373e13c10666763238131307
QT_CVE=CVE-2026-105924-qtbase-6.12.diff # JSON/CBOR: an empty URL tag crashed toJson
QT_CVE_SHA256=c27d27b86aaf2cdab0c18d513fbb94a432e98782eddfc5f856387e4be68be50b
DEJAVU_VERSION=2.37
DEJAVU_SHA256=fa9ca4d13871dd122f61258a80d01751d603b4d3ee14095d65453b4e846e17d7

fetch() { # url file sha256
  if [ ! -f "$CACHE/dl/$2" ]; then
    mkdir -p "$CACHE/dl"
    echo "qt: downloading $1"
    curl -fsSL -o "$CACHE/dl/$2.part" "$1"
    mv "$CACHE/dl/$2.part" "$CACHE/dl/$2"
  fi
  echo "$3  $CACHE/dl/$2" | sha256sum -c --quiet - || { echo "qt: $2 does not match the pinned sha256"; exit 1; }
}

QT_SRC_NAME="qtbase-everywhere-src-$QT_VERSION"
fetch "https://download.qt.io/official_releases/qt/${QT_VERSION%.*}/$QT_VERSION/submodules/$QT_SRC_NAME.tar.xz" \
  "$QT_SRC_NAME.tar.xz" "$QT_SHA256"
fetch "https://download.qt.io/official_releases/qt/${QT_VERSION%.*}/$QT_CVE" "$QT_CVE" "$QT_CVE_SHA256"
fetch "https://github.com/dejavu-fonts/dejavu-fonts/releases/download/version_${DEJAVU_VERSION//./_}/dejavu-fonts-ttf-$DEJAVU_VERSION.tar.bz2" \
  "dejavu-fonts-ttf-$DEJAVU_VERSION.tar.bz2" "$DEJAVU_SHA256"
CROSS="$PORTS/cross/x86_64-linux-musl-cross"
[ -x "$CROSS/bin/x86_64-linux-musl-g++" ] || { echo "qt: the musl cross toolchain is missing: run make ports first"; exit 1; }

# Unpack once; the security diff and our patches (user/qt/patches, each with
# a note saying why) go in before anything is built.
SRC="$CACHE/src/$QT_SRC_NAME"
if [ ! -f "$SRC/.burrow-patched" ]; then
  rm -rf "$SRC"
  mkdir -p "$CACHE/src"
  tar -xf "$CACHE/dl/$QT_SRC_NAME.tar.xz" -C "$CACHE/src" --no-same-owner
  patch -d "$SRC" -p1 -s < "$CACHE/dl/$QT_CVE"
  shopt -s nullglob
  for p in "$ROOT"/user/qt/patches/*.patch; do
    patch -d "$SRC" -p1 -s < "$p"
  done
  shopt -u nullglob
  touch "$SRC/.burrow-patched"
fi
mkdir -p "$CACHE/src"
[ -d "$CACHE/src/dejavu-fonts-ttf-$DEJAVU_VERSION" ] ||
  tar -xf "$CACHE/dl/dejavu-fonts-ttf-$DEJAVU_VERSION.tar.bz2" -C "$CACHE/src" --no-same-owner

# What Burrow lacks or does not need, switched off in both builds so that
# moc sees the same features (bundled libraries, so nothing is probed).
COMMON=(-release -nomake examples -nomake tests -no-opengl -no-feature-vulkan -no-xcb -no-dbus -no-glib
  -no-icu -no-fontconfig -no-feature-sql -no-openssl -no-libjpeg -qt-zlib -qt-libpng -qt-freetype
  -qt-harfbuzz -qt-pcre)

# 1. The host build: the tools the cross build runs.
HOST="$CACHE/host-$QT_VERSION"
if [ ! -x "$HOST/libexec/moc" ]; then
  rm -rf "$CACHE/build/host" && mkdir -p "$CACHE/build/host"
  (cd "$CACHE/build/host" && "$SRC/configure" -prefix "$HOST" "${COMMON[@]}" -qpa offscreen > configure.log 2>&1) ||
    { tail -40 "$CACHE/build/host/configure.log"; exit 1; }
  cmake --build "$CACHE/build/host" --parallel "$JOBS" > "$CACHE/build/host.log" 2>&1 || { tail -40 "$CACHE/build/host.log"; exit 1; }
  cmake --install "$CACHE/build/host" > /dev/null
  echo "qt: host tools -> $HOST"
fi

# 2. The Burrow build: static, musl, linuxfb (and offscreen, for the host checks).
TARGET="$CACHE/target-$QT_VERSION"
TOOLCHAIN="$CACHE/musl-toolchain.cmake"
cat > "$TOOLCHAIN" <<EOF
set(CMAKE_SYSTEM_NAME Linux)
set(CMAKE_SYSTEM_PROCESSOR x86_64)
set(CMAKE_C_COMPILER $CROSS/bin/x86_64-linux-musl-gcc)
set(CMAKE_CXX_COMPILER $CROSS/bin/x86_64-linux-musl-g++)
set(CMAKE_SYSROOT $CROSS/x86_64-linux-musl)
set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)
set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_PACKAGE ONLY)
set(CMAKE_EXE_LINKER_FLAGS_INIT "-static")
EOF
if [ ! -f "$TARGET/lib/libQt6Widgets.a" ]; then
  rm -rf "$CACHE/build/target" && mkdir -p "$CACHE/build/target"
  (cd "$CACHE/build/target" && PKG_CONFIG_LIBDIR=/nonexistent "$SRC/configure" -prefix "$TARGET" -static "${COMMON[@]}" \
    -qt-host-path "$HOST" -qpa linuxfb -no-libinput -no-libudev -no-feature-inotify -no-tslib -no-mtdev \
    -no-xkbcommon -no-feature-gtk3 -- -DCMAKE_TOOLCHAIN_FILE="$TOOLCHAIN" > configure.log 2>&1) ||
    { tail -40 "$CACHE/build/target/configure.log"; exit 1; }
  cmake --build "$CACHE/build/target" --parallel "$JOBS" > "$CACHE/build/target.log" 2>&1 || { tail -40 "$CACHE/build/target.log"; exit 1; }
  cmake --install "$CACHE/build/target" > /dev/null
  echo "qt: static qtbase for Burrow -> $TARGET"
fi

# 3. Our programs (user/qt), and the tree that goes on the Qt data disk.
[ -n "${QT_LIBS_ONLY:-}" ] && exit 0
# Qt's own toolchain file chains to ours and knows where Qt is.
cmake -S "$ROOT/user/qt" -B "$CACHE/build/apps" -G Ninja -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_TOOLCHAIN_FILE="$TARGET/lib/cmake/Qt6/qt.toolchain.cmake" > "$CACHE/build/apps.log" 2>&1 ||
  { tail -40 "$CACHE/build/apps.log"; exit 1; }
cmake --build "$CACHE/build/apps" >> "$CACHE/build/apps.log" 2>&1 || { tail -40 "$CACHE/build/apps.log"; exit 1; }
mkdir -p "$OUT/bin" "$OUT/fonts"
for app in qthello qtwindow qtdemo qtbench; do
  "$CROSS/bin/x86_64-linux-musl-strip" -o "$OUT/bin/$app" "$CACHE/build/apps/$app"
  echo "qt: $app ($(du -h "$OUT/bin/$app" | cut -f1)) -> $OUT/bin/$app"
done
cp "$CACHE/src/dejavu-fonts-ttf-$DEJAVU_VERSION/ttf/"{DejaVuSans.ttf,DejaVuSansMono.ttf} "$OUT/fonts/"
cp "$ROOT/user/qt/qt.env" "$OUT/qt.env"
echo "qt: fonts -> $OUT/fonts"
