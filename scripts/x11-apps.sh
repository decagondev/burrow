# x11-apps.sh - the second half of scripts/x11.sh (W16-S01), sourced by it
# with its variables set: the core fonts (BDF to PCF with the host's
# bdftopcf and mkfontdir), the TinyX Xfbdev server, dwm, st and dmenu with
# user/x11's config.h files, and user/x11's own programs, all static, into
# $OUT, the tree for /opt/x11. Never edits the downloaded sources: builds
# happen in copies of them.

XLIBS="-L$SYS/lib -lXft -lfontconfig -lexpat -lfreetype -lz -lXrender -lXinerama -lXext -lX11 -lxcb -lXau -lXdmcp"
XINCS="-I$SYS/include -I$SYS/include/freetype2"
mkdir -p "$OUT/bin" "$OUT/fonts/misc" "$OUT/fonts/ttf" "$OUT/etc/fonts"

# The core fonts the server needs: fixed and cursor, as PCF with a fonts.dir.
if [ ! -f "$OUT/fonts/misc/fonts.dir" ]; then
  for d in font-misc-misc-1.1.3 font-cursor-misc-1.0.4; do
    for bdf in "$CACHE/src/$d"/*.bdf; do
      bdftopcf -t "$bdf" | gzip -9n > "$OUT/fonts/misc/$(basename "$bdf" .bdf).pcf.gz"
    done
  done
  cp "$CACHE/src/font-alias-1.0.5/misc/fonts.alias" "$OUT/fonts/misc/fonts.alias"
  mkfontdir "$OUT/fonts/misc"
  echo "x11: core fonts ($(ls "$OUT/fonts/misc" | grep -c pcf) PCF files)"
fi
# Xft's fonts: DejaVu (the same pinned download as W15's).
DEJAVU="$CACHE/src/dejavu-fonts-ttf-2.37/ttf"
cp "$DEJAVU/DejaVuSans.ttf" "$DEJAVU/DejaVuSansMono.ttf" "$DEJAVU/DejaVuSans-Bold.ttf" "$DEJAVU/DejaVuSansMono-Bold.ttf" \
  "$DEJAVU/DejaVuSansMono-Oblique.ttf" "$DEJAVU/DejaVuSansMono-BoldOblique.ttf" "$OUT/fonts/ttf/"
cp "$ROOT/user/x11/fonts.conf" "$OUT/etc/fonts/fonts.conf"
cp "$ROOT/user/x11/x11.env" "$OUT/x11.env"

# TinyX's Xfbdev: autoreconf in a copy of the tree, then one static link.
if [ ! -f "$CACHE/stamp/tinyx" ]; then
  b="$CACHE/build/tinyx"
  rm -rf "$b" && cp -R "$CACHE/src/tinyx-$TINYX" "$b"
  for p in "$ROOT"/user/x11/patches/tinyx-*.patch; do # each says why
    patch -d "$b" -p1 -s < "$p"
  done
  (cd "$b" && ACLOCAL_PATH="$SYS/share/aclocal" autoreconf -fi > autoreconf.log 2>&1) || fail "tinyx autoreconf" "$b/autoreconf.log"
  (cd "$b" && ./configure --host=x86_64-linux-musl --prefix="$CACHE/tinyx-install" --disable-shared \
    --disable-xvesa --enable-xfbdev --disable-xdmcp --disable-xdm-auth-1 --with-fontdir=/opt/x11/fonts \
    --with-default-font-path=/opt/x11/fonts/misc > configure.log 2>&1) || fail "tinyx configure" "$b/configure.log"
  make -C "$b" -j"$JOBS" LDFLAGS="-all-static -L$SYS/lib" > "$b/make.log" 2>&1 || fail "tinyx build" "$b/make.log"
  touch "$CACHE/stamp/tinyx" && echo "x11: Xfbdev"
fi
x86_64-linux-musl-strip -o "$OUT/bin/Xfbdev" "$(find "$CACHE/build/tinyx" -name Xfbdev -type f -perm -u+x | head -1)"

# dwm, st, dmenu: their own Makefiles, in copies, with our config.h.
suckless() { # name version [extra libraries]
  local name=$1 v=$2 extra=${3:-}
  local b="$CACHE/build/$name"
  rm -rf "$b" && cp -R "$CACHE/src/$name-$v" "$b"
  [ -f "$ROOT/user/x11/$name-config.h" ] && cp "$ROOT/user/x11/$name-config.h" "$b/config.h"
  make -C "$b" CC="$CC" INCS="$XINCS" LIBS="-static $XLIBS $extra" > "$b/make.log" 2>&1 || fail "$name build" "$b/make.log"
}
suckless dwm 6.5
suckless st 0.9.2 "-lm -lrt -lutil"
suckless dmenu 5.3
for p in dwm/dwm st/st dmenu/dmenu dmenu/stest; do
  x86_64-linux-musl-strip -o "$OUT/bin/$(basename "$p")" "$CACHE/build/$p"
done
cp "$CACHE/build/dmenu/dmenu_run" "$CACHE/build/dmenu/dmenu_path" "$OUT/bin/"
echo "x11: dwm, st, dmenu"

# Our own X programs.
for c in "$ROOT"/user/x11/*.c; do
  n=$(basename "$c" .c)
  "$CC" -static -O2 $XINCS -o "$OUT/bin/$n" "$c" $XLIBS
  x86_64-linux-musl-strip "$OUT/bin/$n"
done
echo "x11: $(ls "$OUT/bin" | wc -l) programs, $(du -sh "$OUT" | cut -f1) -> $OUT"
