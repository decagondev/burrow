# browser-netsurf.sh - the second half of scripts/browser.sh (W24-S04), run
# by it with its variables: NetSurf's own libraries from the netsurf-all
# bundle, each installed into $NSP with NetSurf's buildsystem (static, for
# x86_64-linux-musl), then NetSurf's framebuffer front end on libnsfb's X
# surface with its built-in bitmap font, JavaScript, libnslog, JPEG and
# WebP off, curl without OpenSSL. Never run alone.
# Docs: docs/04-plan/waves/WAVE-24-ssh-browser.md.

NS="$CACHE/src/netsurf-all-$NETSURF"
export CFLAGS="$CFLAGS $CPPFLAGS" # the buildsystem reads CFLAGS only (libdom's expat.h is in the X sysroot)
NSMAKE=(HOST=x86_64-linux-musl PREFIX="$NSP" NSSHARED="$NS/buildsystem" Q= DESTDIR=
  WARNFLAGS="-Wall -W -Wno-error" COMPONENT_TYPE=lib-static)

# nslib builds and installs one library of the bundle.
nslib() { # name
  [ -f "$CACHE/stamp/ns-$1" ] && return
  local log="$CACHE/build/ns-$1.log"
  make -C "$NS/$1" -j"$JOBS" install "${NSMAKE[@]}" > "$log" 2>&1 || fail "$1" "$log"
  touch "$CACHE/stamp/ns-$1"
  echo "browser: $1"
}
if [ ! -f "$CACHE/stamp/ns-buildsystem" ]; then
  make -C "$NS/buildsystem" install PREFIX="$NSP" DESTDIR= > "$CACHE/build/ns-buildsystem.log" 2>&1 ||
    fail "buildsystem" "$CACHE/build/ns-buildsystem.log"
  touch "$CACHE/stamp/ns-buildsystem"
fi
for lib in libwapcaplet libparserutils libhubbub libdom libcss libnsutils libutf8proc libnspsl libnsbmp libnsgif \
  libsvgtiny libnsfb; do
  nslib "$lib"
done

# NetSurf's host tools (convert_image, split-messages) need zlib and libpng
# for the build machine: built from the same pinned sources (zlib is W16's).
HOSTP="$CACHE/build/hostprefix"
if [ ! -f "$CACHE/stamp/host-png" ]; then
  log="$CACHE/build/host-png.log"
  rm -rf "$CACHE/build/host-zlib" "$CACHE/build/host-png" && mkdir -p "$CACHE/build/host-png"
  cp -R "${X11_CACHE:-$HOME/.cache/burrow-x11}/src/zlib-1.3.1" "$CACHE/build/host-zlib"
  (cd "$CACHE/build/host-zlib" && env -u CFLAGS -u CPPFLAGS -u LDFLAGS CC=gcc ./configure --static --prefix="$HOSTP" &&
    make -j"$JOBS" install) > "$log" 2>&1 || fail "host zlib" "$log"
  (cd "$CACHE/build/host-png" && env -u CFLAGS CC=gcc AR=ar RANLIB=ranlib CPPFLAGS="-I$HOSTP/include" LDFLAGS="-L$HOSTP/lib" \
    "$CACHE/src/libpng-1.6.44/configure" --prefix="$HOSTP" --disable-shared && make -j"$JOBS" install) >> "$log" 2>&1 ||
    fail "host libpng" "$log"
  touch "$CACHE/stamp/host-png"
fi

# NetSurf itself: the framebuffer front end, its resources where the disk
# puts them.
NSOPTS=(TARGET=framebuffer NETSURF_FB_FRONTEND=x NETSURF_FB_FONTLIB=internal
  NETSURF_USE_DUKTAPE=NO NETSURF_USE_OPENSSL=NO NETSURF_USE_JPEG=NO NETSURF_USE_JPEGXL=NO NETSURF_USE_WEBP=NO
  NETSURF_USE_NSLOG=NO NETSURF_USE_ROSPRITE=NO NETSURF_USE_VIDEO=NO
  NETSURF_FB_RESPATH="\$\${NETSURFRES}:$MOUNT/share/netsurf" NETSURF_FRAMEBUFFER_RESOURCES="$MOUNT/share/netsurf"
  PKGCONFIG="pkg-config --static" # static: the X libraries' private ones too
  BUILD_CFLAGS="-g -I$HOSTP/include" BUILD_LDFLAGS="-L$HOSTP/lib"
  BUILD_LIBPNG_CFLAGS="-I$HOSTP/include" BUILD_LIBPNG_LDFLAGS="-L$HOSTP/lib -lpng -lz -lm")
STAMP="$CACHE/stamp/netsurf-$NETSURF-$(echo "${NSOPTS[*]}" | sha256sum | cut -c1-12)" # new options: a new build
if [ ! -f "$STAMP" ]; then
  log="$CACHE/build/netsurf.log"
  make -C "$NS/netsurf" clean "${NSMAKE[@]}" TARGET=framebuffer > /dev/null 2>&1 || true # HOST names the build directory
  # -static from the environment: the Makefile adds its libraries to LDFLAGS
  LDFLAGS="-static $LDFLAGS" make -C "$NS/netsurf" -j"$JOBS" "${NSMAKE[@]}" "${NSOPTS[@]}" > "$log" 2>&1 || fail "netsurf" "$log"
  touch "$STAMP"
fi
mkdir -p "$OUT/bin" "$OUT/share/netsurf"
x86_64-linux-musl-strip -o "$OUT/bin/netsurf" "$NS/netsurf/nsfb"
cp -RL "$NS/netsurf/frontends/framebuffer/res/." "$OUT/share/netsurf/"
echo "browser: NetSurf $NETSURF (framebuffer front end on X) -> $OUT/bin/netsurf"
