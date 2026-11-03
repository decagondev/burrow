#!/usr/bin/env bash
# ports.sh - fetch, verify and build the downloaded ports (W12-S04..S06):
# Lua 5.4, a C++ test built with a musl cross toolchain (libstdc++,
# exceptions, std::thread), and TinyCC with the C library it needs to
# compile programs on Burrow. Downloads go once into $PORTS_CACHE (default
# ~/.cache/burrow-ports) and are refused unless their sha256 matches the
# pin (Lua's is lua.org's; TinyCC's and the toolchain's are pinned here, as
# neither publishes one). TinyCC is a development snapshot (its last
# release, 0.9.27 from 2017, mislinks musl objects from a modern GCC: calls
# through R_X86_64_PLT32 land on garbage, on Linux as on Burrow), taken from
# the GitHub mirror of its mob branch at a pinned commit. Lua builds from
# its unpacked source into a separate directory; TinyCC builds in copies of
# its source tree (its runtime's Makefile needs that). The result is a tree laid out like the target's
# filesystem: <out>/bin/{lua,cxxtest,tcc}, <out>/usr/{include,lib}.
# Docs: docs/04-plan/waves/WAVE-12-ports.md, docs/06-guides/PORTING.md.
#
# Usage: scripts/ports.sh <out-dir>
set -euo pipefail
OUT="$(mkdir -p "${1:?usage: ports.sh <out-dir>}" && cd "$1" && pwd)"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE="${PORTS_CACHE:-$HOME/.cache/burrow-ports}"
LUA_VERSION=5.4.9
LUA_SHA256=2335b6c582a52654f94612bf10d2f4672805d05329aa6568b1d8cd9e5c6fb8e6
TCC_COMMIT=e23dabed154a8495f3ac1ff3cc9d04fa1c207893 # mob, 2026-10-16
TCC_SHA256=d2470fdeed862ddb18f4ec4e6a405fa984580c3d85c38492516749c61931c30e
CROSS_SHA256=c5d410d9f82a4f24c549fe5d24f988f85b2679b452413a9f7e5f7b956f2fe7ea # musl.cc, GCC 11.2.1

fetch() { # url file sha256
  if [ ! -f "$CACHE/dl/$2" ]; then
    mkdir -p "$CACHE/dl"
    echo "ports: downloading $1"
    curl -fsSL -o "$CACHE/dl/$2.part" "$1"
    mv "$CACHE/dl/$2.part" "$CACHE/dl/$2"
  fi
  echo "$3  $CACHE/dl/$2" | sha256sum -c --quiet - || { echo "ports: $2 does not match the pinned sha256"; exit 1; }
}
unpack() { # file dir-it-makes [where]
  local where="${3:-$CACHE/src}"
  [ -d "$where/$2" ] && return
  mkdir -p "$where"
  tar -xf "$CACHE/dl/$1" -C "$where" --no-same-owner
}

fetch "https://www.lua.org/ftp/lua-$LUA_VERSION.tar.gz" "lua-$LUA_VERSION.tar.gz" "$LUA_SHA256"
fetch "https://codeload.github.com/TinyCC/tinycc/tar.gz/$TCC_COMMIT" "tinycc-$TCC_COMMIT.tar.gz" "$TCC_SHA256"
fetch "https://musl.cc/x86_64-linux-musl-cross.tgz" "x86_64-linux-musl-cross.tgz" "$CROSS_SHA256"
unpack "lua-$LUA_VERSION.tar.gz" "lua-$LUA_VERSION"
unpack "tinycc-$TCC_COMMIT.tar.gz" "tinycc-$TCC_COMMIT"
unpack "x86_64-linux-musl-cross.tgz" "x86_64-linux-musl-cross" "$CACHE/cross"
CROSS="$CACHE/cross/x86_64-linux-musl-cross"
CC="$CROSS/bin/x86_64-linux-musl-gcc"
CXX="$CROSS/bin/x86_64-linux-musl-g++"
SYSROOT="$CROSS/x86_64-linux-musl"
mkdir -p "$OUT/bin" "$OUT/usr/lib" "$OUT/usr/include"

# Lua: every file but luac.c, POSIX features, no dlopen (static).
LUA_SRC="$CACHE/src/lua-$LUA_VERSION/src"
LUA_BUILD="$CACHE/build/lua-$LUA_VERSION"
mkdir -p "$LUA_BUILD"
for f in "$LUA_SRC"/*.c; do
  [ "$(basename "$f")" = luac.c ] && continue
  o="$LUA_BUILD/$(basename "$f" .c).o"
  [ -f "$o" ] || "$CC" -O2 -std=gnu99 -DLUA_USE_POSIX -c "$f" -o "$o"
done
"$CC" -static -o "$OUT/bin/lua" "$LUA_BUILD"/*.o -lm
echo "ports: Lua $LUA_VERSION -> $OUT/bin/lua"

# C++: our test program against the toolchain's static libstdc++.
"$CXX" -static -s -O2 -std=c++17 -o "$OUT/bin/cxxtest" "$ROOT/user/cxx/cxxtest.cpp" -pthread
echo "ports: C++ (g++ $("$CXX" -dumpversion), static libstdc++) -> $OUT/bin/cxxtest"

# TinyCC: a static musl tcc whose paths are the target's (/usr/include,
# /usr/lib), plus that C library: the toolchain's musl headers and archives.
TCC_SRC="$CACHE/src/tinycc-$TCC_COMMIT"
TCC_BUILD="$CACHE/build/tinycc-$TCC_COMMIT"
# Two configurations: one whose paths are the toolchain's sysroot builds
# libtcc1.a on this host (tcc compiles its own runtime, and the target's
# /usr/include here is glibc's); the one shipped has the target's paths.
tcc_config() { # build-dir include-paths lib-paths crt-prefix target...
  local dir="$1" inc="$2" lib="$3" crt="$4"; shift 4
  if [ ! -f "$dir/.done-$*" ]; then
    rm -rf "$dir" && cp -R "$TCC_SRC" "$dir" # its lib/ Makefile builds only in the source tree
    (cd "$dir" && ./configure --cc="$CC -static" --config-musl --extra-ldflags=-static \
       --prefix=/usr --bindir=/bin --sysincludepaths="$inc" --libpaths="$lib" --crtprefix="$crt" >/dev/null \
       && make -s "$@" >/dev/null && touch ".done-$*")
  fi
}
tcc_config "$TCC_BUILD-host" "{B}/include:$SYSROOT/include" "{B}:$SYSROOT/lib" "$SYSROOT/lib" tcc libtcc1.a
tcc_config "$TCC_BUILD" "/usr/lib/tcc/include:/usr/include" "/usr/lib/tcc:/usr/lib" "/usr/lib" tcc
cp "$TCC_BUILD/tcc" "$OUT/bin/tcc" # installed by hand: make install also wants makeinfo for the manual
rm -rf "$OUT/usr/lib/tcc" && mkdir -p "$OUT/usr/lib/tcc/include"
for f in libtcc1.a runmain.o bt-exe.o bt-log.o bcheck.o; do cp "$TCC_BUILD-host/$f" "$OUT/usr/lib/tcc/"; done # -run needs runmain.o
cp "$TCC_SRC/include/"*.h "$OUT/usr/lib/tcc/include/"
rm -rf "$OUT/usr/include" && mkdir -p "$OUT/usr/include"
(cd "$SYSROOT/include" && find . -path ./linux -prune -o -path ./asm -prune -o -path ./asm-generic -prune \
   -o -path ./c++ -prune -o -path ./mtd -prune -o -path ./drm -prune -o -path ./rdma -prune -o -path ./sound -prune \
   -o -path ./video -prune -o -path ./xen -prune -o -path ./scsi -prune -o -print | cpio -pdm --quiet "$OUT/usr/include")
for f in crt1.o crti.o crtn.o libc.a libm.a libpthread.a libdl.a librt.a; do cp "$SYSROOT/lib/$f" "$OUT/usr/lib/"; done
mkdir -p "$OUT/usr/share/burrow"
cp "$ROOT/user/tcc/"*.c "$OUT/usr/share/burrow/"
echo "ports: TinyCC ${TCC_COMMIT:0:7} -> $OUT/bin/tcc (+ musl headers and libc.a in $OUT/usr)"
du -sh "$OUT" | sed 's/^/ports: /'
