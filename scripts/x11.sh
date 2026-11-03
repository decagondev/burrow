#!/usr/bin/env bash
# x11.sh - fetch, verify and build the X11 desktop for Burrow (W16-S01):
# the X libraries (Xlib, xcb, Xft, fontconfig, FreeType, libXfont 1),
# the core fonts, the TinyX Xfbdev server, dwm, st and dmenu, all static
# against musl with the ports' toolchain (scripts/ports.sh), plus
# user/x11's programs. Downloads go once into $X11_CACHE (default
# ~/.cache/burrow-x11) and are refused unless their sha256 matches the pin;
# none of these projects publishes a sha256 beside the file (zlib's matches
# zlib.net's, DejaVu's is W15's), so the pins are the files as first
# downloaded on 2026-11-03.
# Each package builds once into a sysroot in the cache and leaves a stamp, so
# a rerun picks up where a failure stopped. Never run inside make test.
# Output: <out>/bin, fonts/, etc/fonts, the tree for /opt/x11.
# Docs: docs/04-plan/waves/WAVE-16-x11-desktop.md, PORTING.md.
#
# Usage: scripts/x11.sh <out-dir>
set -euo pipefail
OUT="$(mkdir -p "${1:?usage: x11.sh <out-dir>}" && cd "$1" && pwd)"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE="${X11_CACHE:-$HOME/.cache/burrow-x11}"
PORTS="${PORTS_CACHE:-$HOME/.cache/burrow-ports}"
QTCACHE="${QT_CACHE:-$HOME/.cache/burrow-qt}"
JOBS="${JOBS:-$(nproc)}"
SYS="$CACHE/sysroot"
CROSS="$PORTS/cross/x86_64-linux-musl-cross"
TINYX=feab72ca891bc04b18763763e15ee4e532369cdf # tinycorelinux/tinyx master, 2024-11-13

# file sha256 url
SOURCES="
zlib-1.3.1.tar.gz 9a93b2b7dfdac77ceba5a558a580e74667dd6fede4585b91eefb60f03b72df23 https://zlib.net/fossils/zlib-1.3.1.tar.gz
expat-2.6.4.tar.xz a695629dae047055b37d50a0ff4776d1d45d0a4c842cf4ccee158441f55ff7ee https://github.com/libexpat/libexpat/releases/download/R_2_6_4/expat-2.6.4.tar.xz
freetype-2.13.3.tar.xz 0550350666d427c74daeb85d5ac7bb353acba5f76956395995311a9c6f063289 https://download.savannah.gnu.org/releases/freetype/freetype-2.13.3.tar.xz
xorgproto-2024.1.tar.xz 372225fd40815b8423547f5d890c5debc72e88b91088fbfb13158c20495ccb59 https://www.x.org/releases/individual/proto/xorgproto-2024.1.tar.xz
xtrans-1.5.2.tar.xz 5c5cbfe34764a9131d048f03c31c19e57fb4c682d67713eab6a65541b4dff86c https://www.x.org/releases/individual/lib/xtrans-1.5.2.tar.xz
libXau-1.0.12.tar.xz 74d0e4dfa3d39ad8939e99bda37f5967aba528211076828464d2777d477fc0fb https://www.x.org/releases/individual/lib/libXau-1.0.12.tar.xz
libXdmcp-1.1.5.tar.xz d8a5222828c3adab70adf69a5583f1d32eb5ece04304f7f8392b6a353aa2228c https://www.x.org/releases/individual/lib/libXdmcp-1.1.5.tar.xz
libpthread-stubs-0.5.tar.xz 59da566decceba7c2a7970a4a03b48d9905f1262ff94410a649224e33d2442bc https://xcb.freedesktop.org/dist/libpthread-stubs-0.5.tar.xz
xcb-proto-1.17.0.tar.xz 2c1bacd2110f4799f74de6ebb714b94cf6f80fb112316b1219480fd22562148c https://xorg.freedesktop.org/archive/individual/proto/xcb-proto-1.17.0.tar.xz
libxcb-1.17.0.tar.xz 599ebf9996710fea71622e6e184f3a8ad5b43d0e5fa8c4e407123c88a59a6d55 https://xorg.freedesktop.org/archive/individual/lib/libxcb-1.17.0.tar.xz
libX11-1.8.12.tar.xz fa026f9bb0124f4d6c808f9aef4057aad65e7b35d8ff43951cef0abe06bb9a9a https://www.x.org/releases/individual/lib/libX11-1.8.12.tar.xz
libXext-1.3.6.tar.xz edb59fa23994e405fdc5b400afdf5820ae6160b94f35e3dc3da4457a16e89753 https://www.x.org/releases/individual/lib/libXext-1.3.6.tar.xz
libXrender-0.9.12.tar.xz b832128da48b39c8d608224481743403ad1691bf4e554e4be9c174df171d1b97 https://www.x.org/releases/individual/lib/libXrender-0.9.12.tar.xz
libXinerama-1.1.5.tar.xz 5094d1f0fcc1828cb1696d0d39d9e866ae32520c54d01f618f1a3c1e30c2085c https://www.x.org/releases/individual/lib/libXinerama-1.1.5.tar.xz
fontconfig-2.15.0.tar.xz 63a0658d0e06e0fa886106452b58ef04f21f58202ea02a94c39de0d3335d7c0e https://www.freedesktop.org/software/fontconfig/release/fontconfig-2.15.0.tar.xz
libXft-2.3.9.tar.xz 60a25b78945ed6932635b3bb1899a517d31df7456e69867ffba27f89ff3976f5 https://www.x.org/releases/individual/lib/libXft-2.3.9.tar.xz
libfontenc-1.1.8.tar.xz 7b02c3d405236e0d86806b1de9d6868fe60c313628b38350b032914aa4fd14c6 https://www.x.org/releases/individual/lib/libfontenc-1.1.8.tar.xz
libXfont-1.5.4.tar.bz2 1a7f7490774c87f2052d146d1e0e64518d32e6848184a18654e8d0bb57883242 https://www.x.org/releases/individual/lib/libXfont-1.5.4.tar.bz2
font-misc-misc-1.1.3.tar.xz 79abe361f58bb21ade9f565898e486300ce1cc621d5285bec26e14b6a8618fed https://www.x.org/releases/individual/font/font-misc-misc-1.1.3.tar.xz
font-cursor-misc-1.0.4.tar.xz 25d9c9595013cb8ca08420509993a6434c917e53ca1fec3f63acd45a19d4f982 https://www.x.org/releases/individual/font/font-cursor-misc-1.0.4.tar.xz
font-alias-1.0.5.tar.xz 9f89e217bb73e0e3636a0a493fbf8b7c995156e0c53d9a0476d201b67c2d6b6e https://www.x.org/releases/individual/font/font-alias-1.0.5.tar.xz
tinyx-$TINYX.tar.gz 82f69bbd7a979d30a25a80082ba6fdcb975740ba36836795a3763814aff0adca https://codeload.github.com/tinycorelinux/tinyx/tar.gz/$TINYX
dwm-6.5.tar.gz 21d79ebfa9f2fb93141836c2666cb81f4784c69d64e7f1b2352f9b970ba09729 https://dl.suckless.org/dwm/dwm-6.5.tar.gz
st-0.9.2.tar.gz 6b215d4f472b21d6232f30f221117a777e24bcfee68955ddefb7426467f9494b https://dl.suckless.org/st/st-0.9.2.tar.gz
dmenu-5.3.tar.gz 1a8f53e6fd2d749839ec870c5e27b3e14da5c3eeacbfcb945d159e1d5433795f https://dl.suckless.org/tools/dmenu-5.3.tar.gz
dejavu-fonts-ttf-2.37.tar.bz2 fa9ca4d13871dd122f61258a80d01751d603b4d3ee14095d65453b4e846e17d7 https://github.com/dejavu-fonts/dejavu-fonts/releases/download/version_2_37/dejavu-fonts-ttf-2.37.tar.bz2
"

fail() { echo "x11: $1 failed; the end of $2:"; tail -30 "$2"; exit 1; }

mkdir -p "$CACHE/dl" "$CACHE/src" "$CACHE/build" "$CACHE/stamp" "$SYS"
# DejaVu is the same pinned file W15 fetched: copy it rather than fetch it again.
[ -f "$QTCACHE/dl/dejavu-fonts-ttf-2.37.tar.bz2" ] && [ ! -f "$CACHE/dl/dejavu-fonts-ttf-2.37.tar.bz2" ] &&
  cp "$QTCACHE/dl/dejavu-fonts-ttf-2.37.tar.bz2" "$CACHE/dl/"
while read -r file sum url; do
  [ -n "$file" ] || continue
  if [ ! -f "$CACHE/dl/$file" ]; then
    echo "x11: downloading $url"
    curl -fsSL -o "$CACHE/dl/$file.part" "$url"
    mv "$CACHE/dl/$file.part" "$CACHE/dl/$file"
  fi
  echo "$sum  $CACHE/dl/$file" | sha256sum -c --quiet - || { echo "x11: $file does not match the pinned sha256"; exit 1; }
  dir="${file%.tar.*}"
  [ -d "$CACHE/src/$dir" ] || tar -xf "$CACHE/dl/$file" -C "$CACHE/src" --no-same-owner
done <<< "$SOURCES"
[ -x "$CROSS/bin/x86_64-linux-musl-gcc" ] || { echo "x11: the musl cross toolchain is missing: run make ports first"; exit 1; }

export PATH="$CROSS/bin:$PATH"
export CC=x86_64-linux-musl-gcc AR=x86_64-linux-musl-ar RANLIB=x86_64-linux-musl-ranlib
export CFLAGS="-O2" CPPFLAGS="-I$SYS/include" LDFLAGS="-L$SYS/lib"
export PKG_CONFIG_LIBDIR="$SYS/lib/pkgconfig:$SYS/share/pkgconfig" PKG_CONFIG_PATH=
export CC_FOR_BUILD=gcc # libX11's makekeys runs on the host

# auto builds an autotools package from $CACHE/src/<src> into the sysroot.
auto() { # stamp src [configure arguments]
  local name=$1 src=$2
  shift 2
  [ -f "$CACHE/stamp/$name" ] && return
  local b="$CACHE/build/$name"
  rm -rf "$b" && mkdir -p "$b"
  (cd "$b" && "$CACHE/src/$src/configure" --host=x86_64-linux-musl --prefix="$SYS" \
    --disable-shared --enable-static "$@" > configure.log 2>&1) || fail "$name configure" "$b/configure.log"
  make -C "$b" -j"$JOBS" > "$b/make.log" 2>&1 || fail "$name build" "$b/make.log"
  make -C "$b" install > "$b/install.log" 2>&1 || fail "$name install" "$b/install.log"
  touch "$CACHE/stamp/$name"
  echo "x11: $name"
}

# zlib has its own configure, run in a copy of its tree.
if [ ! -f "$CACHE/stamp/zlib" ]; then
  rm -rf "$CACHE/build/zlib" && cp -R "$CACHE/src/zlib-1.3.1" "$CACHE/build/zlib"
  (cd "$CACHE/build/zlib" && ./configure --static --prefix="$SYS" > configure.log 2>&1 && make -j"$JOBS" > make.log 2>&1 &&
    make install > install.log 2>&1) || fail zlib "$CACHE/build/zlib/make.log"
  touch "$CACHE/stamp/zlib" && echo "x11: zlib"
fi
auto expat expat-2.6.4 --without-docbook --without-examples --without-tests --without-xmlwf
auto freetype freetype-2.13.3 --with-zlib=yes --with-png=no --with-bzip2=no --with-harfbuzz=no --with-brotli=no
auto xorgproto xorgproto-2024.1
auto xtrans xtrans-1.5.2
auto libXau libXau-1.0.12
auto libXdmcp libXdmcp-1.1.5 --disable-docs
auto pthread-stubs libpthread-stubs-0.5
auto xcb-proto xcb-proto-1.17.0 PYTHON=python3
auto libxcb libxcb-1.17.0 --disable-devel-docs --without-doxygen PYTHON=python3
auto libX11 libX11-1.8.12 --enable-malloc0returnsnull --disable-specs --without-xmlto --without-fop --disable-xf86bigfont
auto libXext libXext-1.3.6 --enable-malloc0returnsnull --disable-specs --without-xmlto --without-fop
auto libXrender libXrender-0.9.12 --enable-malloc0returnsnull
auto libXinerama libXinerama-1.1.5 --enable-malloc0returnsnull
# fontconfig reads its configuration from /opt/x11 on Burrow: install to a
# staging tree and keep the sysroot's half.
if [ ! -f "$CACHE/stamp/fontconfig" ]; then
  b="$CACHE/build/fontconfig"
  rm -rf "$b" "$CACHE/fc-stage" && mkdir -p "$b"
  (cd "$b" && "$CACHE/src/fontconfig-2.15.0/configure" --host=x86_64-linux-musl --prefix="$SYS" --disable-shared \
    --enable-static --disable-docs --disable-cache-build --disable-nls --sysconfdir=/opt/x11/etc \
    --localstatedir=/opt/x11/var --with-default-fonts=/opt/x11/fonts/ttf \
    --with-cache-dir=/tmp/fontconfig > configure.log 2>&1) || fail "fontconfig configure" "$b/configure.log"
  make -C "$b" -j"$JOBS" > "$b/make.log" 2>&1 || fail "fontconfig build" "$b/make.log"
  make -C "$b" install DESTDIR="$CACHE/fc-stage" > "$b/install.log" 2>&1 || fail "fontconfig install" "$b/install.log"
  cp -a "$CACHE/fc-stage$SYS/." "$SYS/"
  touch "$CACHE/stamp/fontconfig" && echo "x11: fontconfig"
fi
auto libXft libXft-2.3.9
auto libfontenc libfontenc-1.1.8 --with-encodingsdir=/opt/x11/fonts/encodings
auto libXfont libXfont-1.5.4 --disable-devel-docs --without-xmlto --disable-fc --disable-snfformat --enable-freetype

[ -n "${X11_LIBS_ONLY:-}" ] && exit 0
. "$ROOT/scripts/x11-apps.sh"
