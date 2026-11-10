#!/usr/bin/env bash
# net-ports.sh - fetch, verify and build the network ports (W24-S01): mbedTLS
# (TLS for C programs) and curl over it, static for musl with the ports'
# cross toolchain (make ports first), and curl.se's copy of Mozilla's CA
# bundle. Downloads go once into $NET_CACHE (default ~/.cache/burrow-net)
# and are refused unless their sha256 matches the pin (the files as first
# fetched: mbedTLS's GitHub release, curl.se's tarball and bundle). The
# result is the tree of the NETDATA disk, mounted at /opt/net:
# <out>/bin/curl, <out>/etc/ssl/certs/ca-certificates.crt, <out>/lib and
# <out>/include (mbedTLS for C ports and tcc), <out>/net.env.
# Docs: docs/04-plan/waves/WAVE-24-ssh-browser.md, docs/06-guides/PORTING.md.
#
# Usage: scripts/net-ports.sh <out-dir>
set -euo pipefail
OUT="$(mkdir -p "${1:?usage: net-ports.sh <out-dir>}" && cd "$1" && pwd)"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE="${NET_CACHE:-$HOME/.cache/burrow-net}"
PORTS="${PORTS_CACHE:-$HOME/.cache/burrow-ports}"
JOBS="${JOBS:-$(nproc)}"
MBEDTLS=3.6.2
MBEDTLS_SHA256=8b54fb9bcf4d5a7078028e0520acddefb7900b3e66fec7f7175bb5b7d85ccdca
CURL=8.11.1
CURL_SHA256=c7ca7db48b0909743eaef34250da02c19bc61d4f1dcedd6603f109409536ab56
CACERT=2024-12-31
CACERT_SHA256=a3f328c21e39ddd1f2be1cea43ac0dec819eaa20a90425d7da901a11531b3aa5
MOUNT=/opt/net # where the NETDATA disk is mounted on Burrow

CROSS="$PORTS/cross/x86_64-linux-musl-cross"
[ -x "$CROSS/bin/x86_64-linux-musl-gcc" ] || { echo "net-ports: no musl cross toolchain (make ports first)"; exit 1; }
CC="$CROSS/bin/x86_64-linux-musl-gcc"
AR="$CROSS/bin/x86_64-linux-musl-ar"

fetch() { # url file sha256
  if [ ! -f "$CACHE/dl/$2" ]; then
    mkdir -p "$CACHE/dl"
    echo "net-ports: downloading $1"
    curl -fsSL -o "$CACHE/dl/$2.part" "$1"
    mv "$CACHE/dl/$2.part" "$CACHE/dl/$2"
  fi
  echo "$3  $CACHE/dl/$2" | sha256sum -c --quiet - || { echo "net-ports: $2 does not match the pinned sha256"; exit 1; }
}
unpack() { # file dir-it-makes
  [ -d "$CACHE/src/$2" ] && return
  mkdir -p "$CACHE/src"
  tar -xf "$CACHE/dl/$1" -C "$CACHE/src" --no-same-owner
}
fail() { echo "net-ports: $1 failed; the end of $2:"; tail -30 "$2"; exit 1; }

fetch "https://github.com/Mbed-TLS/mbedtls/releases/download/mbedtls-$MBEDTLS/mbedtls-$MBEDTLS.tar.bz2" "mbedtls-$MBEDTLS.tar.bz2" "$MBEDTLS_SHA256"
fetch "https://curl.se/download/curl-$CURL.tar.xz" "curl-$CURL.tar.xz" "$CURL_SHA256"
fetch "https://curl.se/ca/cacert-$CACERT.pem" "cacert-$CACERT.pem" "$CACERT_SHA256"
unpack "mbedtls-$MBEDTLS.tar.bz2" "mbedtls-$MBEDTLS"
unpack "curl-$CURL.tar.xz" "curl-$CURL"
mkdir -p "$OUT/bin" "$OUT/lib" "$OUT/include" "$OUT/etc/ssl/certs" "$CACHE/build" "$CACHE/stamp"
PREFIX="$CACHE/build/prefix"

# mbedTLS: its own Makefile, the libraries only.
if [ ! -f "$CACHE/stamp/mbedtls-$MBEDTLS" ]; then
  log="$CACHE/build/mbedtls.log"
  make -C "$CACHE/src/mbedtls-$MBEDTLS" -j"$JOBS" lib CC="$CC" AR="$AR" CFLAGS="-O2" > "$log" 2>&1 || fail "mbedTLS" "$log"
  make -C "$CACHE/src/mbedtls-$MBEDTLS" install DESTDIR="$PREFIX" >> "$log" 2>&1 || fail "mbedTLS install" "$log"
  touch "$CACHE/stamp/mbedtls-$MBEDTLS"
fi
echo "net-ports: mbedTLS $MBEDTLS -> $PREFIX/lib"

# curl: HTTP(S) only, over mbedTLS, the bundle where the disk puts it.
if [ ! -f "$CACHE/stamp/curl-$CURL" ]; then
  log="$CACHE/build/curl.log"
  rm -rf "$CACHE/build/curl" && mkdir -p "$CACHE/build/curl"
  (cd "$CACHE/build/curl" && "$CACHE/src/curl-$CURL/configure" --host=x86_64-linux-musl CC="$CC" AR="$AR" \
    CPPFLAGS="-I$PREFIX/include" LDFLAGS="-L$PREFIX/lib -static" \
    --disable-shared --enable-static --with-mbedtls="$PREFIX" --without-libpsl --without-zlib --without-brotli \
    --without-zstd --without-libidn2 --without-nghttp2 --without-libssh2 --disable-ldap --disable-rtsp --disable-dict \
    --disable-telnet --disable-tftp --disable-pop3 --disable-imap --disable-smtp --disable-gopher --disable-mqtt \
    --disable-docs --disable-manual --with-ca-bundle="$MOUNT/etc/ssl/certs/ca-certificates.crt" --without-ca-path \
    > "$log" 2>&1) || fail "curl configure" "$log"
  make -C "$CACHE/build/curl" -j"$JOBS" LDFLAGS="-L$PREFIX/lib -all-static" >> "$log" 2>&1 || fail "curl" "$log"
  touch "$CACHE/stamp/curl-$CURL"
fi
"$CROSS/bin/x86_64-linux-musl-strip" -o "$OUT/bin/curl" "$CACHE/build/curl/src/curl"
echo "net-ports: curl $CURL (mbedTLS) -> $OUT/bin/curl"

cp "$CACHE/dl/cacert-$CACERT.pem" "$OUT/etc/ssl/certs/ca-certificates.crt"
cp -R "$PREFIX/include/." "$OUT/include/"
cp "$PREFIX"/lib/libmbed*.a "$OUT/lib/"
cp "$ROOT/user/net/net.env" "$OUT/net.env"
echo "net-ports: CA bundle $CACERT, mbedTLS headers and libraries, net.env -> $OUT"
