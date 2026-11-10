#!/usr/bin/env bash
# browser.sh - fetch, verify and build NetSurf (W24-S04): NetSurf 3.11's
# framebuffer front end on libnsfb's X surface, static for musl with the
# ports' cross toolchain, over W16's X libraries (make x11: libxcb, zlib,
# FreeType) and S01's curl and mbedTLS (make net-ports), with the xcb-util
# libraries libnsfb's surface needs and libpng. NetSurf's own libraries come
# from the netsurf-all bundle and are built one by one with its buildsystem;
# nsgenbind, libnslog and JavaScript are left out (nsgenbind and libnslog
# need flex and bison, and only JavaScript needs nsgenbind). Downloads go
# once into $NET_CACHE and are refused unless their sha256 matches the pin
# (the files as first fetched). The result lands in the NETDATA tree:
# <out>/bin/netsurf and its resources in <out>/share/netsurf.
# Docs: docs/04-plan/waves/WAVE-24-ssh-browser.md, docs/06-guides/PORTING.md.
#
# Usage: scripts/browser.sh <out-dir>
set -euo pipefail
OUT="$(mkdir -p "${1:?usage: browser.sh <out-dir>}" && cd "$1" && pwd)"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE="${NET_CACHE:-$HOME/.cache/burrow-net}"
PORTS="${PORTS_CACHE:-$HOME/.cache/burrow-ports}"
SYS="${X11_CACHE:-$HOME/.cache/burrow-x11}/sysroot"
JOBS="${JOBS:-$(nproc)}"
NETSURF=3.11
MOUNT=/opt/net # where the NETDATA disk is mounted on Burrow
SOURCES="
netsurf-all-$NETSURF.tar.gz 4dea880ff3c2f698bfd62c982b259340f9abcd7f67e6c8eb2b32c61f71644b7b https://download.netsurf-browser.org/netsurf/releases/source-full/netsurf-all-$NETSURF.tar.gz
xcb-util-0.4.1.tar.xz 5abe3bbbd8e54f0fa3ec945291b7e8fa8cfd3cccc43718f8758430f94126e512 https://xorg.freedesktop.org/archive/individual/lib/xcb-util-0.4.1.tar.xz
xcb-util-image-0.4.1.tar.xz ccad8ee5dadb1271fd4727ad14d9bd77a64e505608766c4e98267d9aede40d3d https://xorg.freedesktop.org/archive/individual/lib/xcb-util-image-0.4.1.tar.xz
xcb-util-keysyms-0.4.1.tar.xz 7c260a5294412aed429df1da2f8afd3bd07b7cba3fec772fba15a613a6d5c638 https://xorg.freedesktop.org/archive/individual/lib/xcb-util-keysyms-0.4.1.tar.xz
xcb-util-wm-0.4.2.tar.xz 62c34e21d06264687faea7edbf63632c9f04d55e72114aa4a57bb95e4f888a0b https://xorg.freedesktop.org/archive/individual/lib/xcb-util-wm-0.4.2.tar.xz
libpng-1.6.44.tar.xz 60c4da1d5b7f0aa8d158da48e8f8afa9773c1c8baa5d21974df61f1886b8ce8e https://download.sourceforge.net/libpng/libpng-1.6.44.tar.xz
"

CROSS="$PORTS/cross/x86_64-linux-musl-cross"
[ -x "$CROSS/bin/x86_64-linux-musl-gcc" ] || { echo "browser: no musl cross toolchain (make ports first)"; exit 1; }
[ -f "$SYS/lib/libxcb.a" ] || { echo "browser: no X libraries (make x11 first)"; exit 1; }
CURLB="$CACHE/build/curl"
[ -f "$CURLB/lib/.libs/libcurl.a" ] || { echo "browser: no libcurl (make net-ports first)"; exit 1; }
fail() { echo "browser: $1 failed; the end of $2:"; tail -30 "$2"; exit 1; }

mkdir -p "$CACHE/dl" "$CACHE/src" "$CACHE/build" "$CACHE/stamp"
while read -r file sum url; do
  [ -n "$file" ] || continue
  if [ ! -f "$CACHE/dl/$file" ]; then
    echo "browser: downloading $url"
    curl -fsSL -o "$CACHE/dl/$file.part" "$url"
    mv "$CACHE/dl/$file.part" "$CACHE/dl/$file"
  fi
  echo "$sum  $CACHE/dl/$file" | sha256sum -c --quiet - || { echo "browser: $file does not match the pinned sha256"; exit 1; }
  dir="${file%.tar.*}"
  [ -d "$CACHE/src/$dir" ] || tar -xf "$CACHE/dl/$file" -C "$CACHE/src" --no-same-owner
done <<< "$SOURCES"

NSP="$CACHE/build/nsprefix" # NetSurf's libraries, over the X sysroot
mkdir -p "$NSP/lib/pkgconfig" "$NSP/include"
export PATH="$CROSS/bin:$PATH"
export CC=x86_64-linux-musl-gcc AR=x86_64-linux-musl-ar RANLIB=x86_64-linux-musl-ranlib
export CFLAGS="-O2" CPPFLAGS="-I$NSP/include -I$SYS/include" LDFLAGS="-L$NSP/lib -L$SYS/lib"
export PKG_CONFIG_LIBDIR="$NSP/lib/pkgconfig:$SYS/lib/pkgconfig:$SYS/share/pkgconfig" PKG_CONFIG_PATH=

# auto builds an autotools package into the NetSurf prefix.
auto() { # stamp src [configure arguments]
  local name=$1 src=$2
  shift 2
  [ -f "$CACHE/stamp/$name" ] && return
  local b="$CACHE/build/$name"
  rm -rf "$b" && mkdir -p "$b"
  (cd "$b" && "$CACHE/src/$src/configure" --host=x86_64-linux-musl --prefix="$NSP" \
    --disable-shared --enable-static "$@" > configure.log 2>&1) || fail "$name configure" "$b/configure.log"
  make -C "$b" -j"$JOBS" > "$b/make.log" 2>&1 || fail "$name build" "$b/make.log"
  make -C "$b" install > "$b/install.log" 2>&1 || fail "$name install" "$b/install.log"
  touch "$CACHE/stamp/$name"
  echo "browser: $name"
}
auto xcb-util xcb-util-0.4.1
auto xcb-util-image xcb-util-image-0.4.1
auto xcb-util-keysyms xcb-util-keysyms-0.4.1
auto xcb-util-wm xcb-util-wm-0.4.2
auto libpng libpng-1.6.44

# libcurl as S01 built it (mbedTLS, HTTP and HTTPS), with a pkg-config file.
cp "$CURLB/lib/.libs/libcurl.a" "$CACHE/build/prefix/lib/"libmbed*.a "$NSP/lib/"
cp -R "$CACHE/src/curl-8.11.1/include/curl" "$NSP/include/"
cp -R "$CACHE/build/prefix/include/." "$NSP/include/"
cat > "$NSP/lib/pkgconfig/libcurl.pc" <<EOF
Name: libcurl
Description: curl from net-ports.sh (mbedTLS)
Version: 8.11.1
Libs: -L$NSP/lib -lcurl -lmbedtls -lmbedx509 -lmbedcrypto
Cflags: -I$NSP/include -DCURL_STATICLIB
EOF

# NetSurf asks pkg-config for libnsfb's libraries without --static, so an
# xcb.pc ahead of the sysroot's names libxcb's own dependencies after it.
sed -e 's/^Libs: \(.*\)/Libs: \1 -lXau -lXdmcp/' "$SYS/lib/pkgconfig/xcb.pc" > "$NSP/lib/pkgconfig/xcb.pc"

. "$ROOT/scripts/browser-netsurf.sh"
