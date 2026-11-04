# Porting software to Burrow

Burrow runs **static Linux x86-64 programs unchanged** ([ADR-0006](../03-decisions/ADR-0006-linux-abi-userland.md)): the Go kernel answers their Linux system calls. Porting is therefore mostly *building* (static, for x86-64 Linux) and, when a program needs a call Burrow lacks, *adding that call's Linux semantics* to `kernel/proc`. This guide is written as ports land (W10-W12); [WORKBENCH.md](WORKBENCH.md) is about using what is here; [POSIX_COVERAGE.md](../02-architecture/POSIX_COVERAGE.md) is the list of what exists.

## The recipe

1. **Build static for Linux.** C: `musl-gcc -static` (the Makefile builds every `user/*.c` this way; `user/doom/` and busybox show bigger builds driven by a script). Go: `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` (every `user/go/<name>/main.go` becomes `/bin/<name>`). Programs that include Linux headers (`<linux/fb.h>`, `<linux/input.h>`) get them from `build/uapi`, which links to the host's kernel UAPI headers and nothing of glibc.
2. **Put it in the initrd** (`/bin` comes from `user/`), or, if it needs a download or is big, on its own image the way `go-busybox.img` and `go-doom.img` are made: the usual initrd plus the program, data on a FAT disk at `/mnt`.
3. **Run it from the console** (`run /bin/prog args`) and read the serial log. An unknown call prints `[proc] WARN: pid N (prog): unimplemented syscall NR` once; a crash prints `killed: page fault at ... (rip ...)`.
4. **Implement what is missing**, with Linux's semantics: the man page, then what the program actually relies on. Add a host test in `kernel/proc` against `proctest.Fake` and a row in POSIX_COVERAGE.md. Compare with Linux by running the same static binary there under `strace -f`.
5. **Keep it running**: a keys file and an expect file under `tests/smoke/` (the port prints a line that says it worked), wired into `make test`; anything downloaded is pinned by sha256 and the check skips without it.

## What ports have needed so far

| Port | Missing piece it found | Wave |
|---|---|---|
| musl C programs | nothing beyond W9's 25 calls | W9 |
| our shell and tools | fork, exec, pipes, signals, a TTY | W10 |
| busybox ash | `poll` (its line editor), `prctl` | W10 |
| Doom | `readv` (musl's `fread`), then only devices: fbdev, evdev | W11 |
| musl pthreads | threads (`clone`, `futex`, per-thread signals), `mprotect` for thread stacks | W12 |
| a Go program | `epoll`, `eventfd`, `sigaltstack` + `SA_ONSTACK`, `madvise`, rlimits, and demand-zero `mmap` (the runtime reserves hundreds of MiB it never touches) | W12 |
| Lua 5.4 | nothing | W12 |
| C++ (libstdc++: exceptions, `std::thread`) | nothing beyond the threads above | W12 |
| TinyCC, compiling on Burrow | nothing in the kernel; its last release mislinks modern musl objects, so a newer snapshot | W12 |

## Toolchains

- **C:** `musl-gcc` (Debian/Ubuntu `musl-tools`) for programs in the tree; `scripts/ports.sh` uses musl.cc's `x86_64-linux-musl-cross` for C and C++ (it carries a complete musl sysroot and libstdc++).
- **C++:** that toolchain's `x86_64-linux-musl-g++ -static`. Exceptions and threads work unchanged.
- **Go:** the host's Go, `GOOS=linux CGO_ENABLED=0`.
- **On Burrow:** `tcc -static` with the musl headers and `libc.a` under `/usr` (`make ports`). Use `-static`: Burrow has no dynamic loader. `tcc -run` is not available in a static `tcc`.
- **When a ported binary crashes, run the same binary on Linux first.** A crash at the same address on both is the program's (or its toolchain's) bug; TinyCC 0.9.27 was such a case. Burrow aims to fail exactly where Linux fails.

## Things that bite

- **Implement the contract, not the call.** The Go runtime needs `MADV_DONTNEED` to zero (BUG-0009), lazy `mmap` reservations, signals on the alternate stack, and `FUTEX_WAIT` to fail with `EAGAIN` when the word changed. A call that returns 0 but does not do what Linux does fails later and far away.
- **Signals and threads.** Masks and alternate stacks are per thread; `tgkill` targets one thread (Go preempts goroutines that way). A process-directed signal goes to the first thread that does not block it.
- **Headers from two worlds.** `<linux/input.h>` and Doom's `doomkeys.h` both define `KEY_ENTER` (BUG-0004): capture one before including the other.
- **A Windows-backed checkout** cannot hold files whose names differ only in case (the Linux UAPI headers have such pairs): link to such trees instead of copying them.
- **Smoke waits** match the serial log, which also holds the echo of what was typed: wait for output the command line does not contain.

## Dynamic linking (W18)

Burrow runs dynamically linked programs since W18: `execve` loads the program and the interpreter its `PT_INTERP` names, starts the interpreter with `AT_BASE` in the auxiliary vector, and musl's loader does the rest. The initrd's `/lib` holds musl's loader, `/lib/ld-musl-x86_64.so.1` (the `libc.so` of the host's `musl-gcc`, `MUSL_LOADER` in the Makefile), and the libraries; `/lib` is on the loader's default path, so nothing needs `LD_LIBRARY_PATH`. To port a dynamic program: build it with `musl-gcc` without `-static` (shared libraries with `-shared -fPIC`), check it on the host's Linux first with `LD_LIBRARY_PATH` at the build's libraries, and put its libraries in the initrd's `/lib`; `user/dyn/` (`dynhello`, a library it links against and a plug-in it opens with `dlopen`) is the example. Everything that existed before W18 is still static, and static stays the simpler way to ship one program; dynamic linking is for sharing code between several and for plug-ins. Shared Qt and X libraries are later work.

## Qt (W15)

Qt widget programs run on Burrow with no X server: Qt's `linuxfb` platform draws on `/dev/fb0` and its own evdev handlers read the keyboard and mouse. `scripts/qt.sh` (`make qt`) builds qtbase 6.12.0 twice: for the host, because a Qt cross build runs the host's `moc`, `rcc` and `uic` (`-qt-host-path`), and statically for Burrow with musl.cc's GCC 11.2.1. The Burrow configure line, beyond `-static -release -nomake examples -nomake tests`:

```
-qpa linuxfb -no-opengl -no-feature-vulkan -no-xcb -no-dbus -no-glib -no-icu -no-fontconfig
-no-feature-sql -no-openssl -no-libjpeg -no-libinput -no-libudev -no-feature-inotify
-no-tslib -no-mtdev -no-xkbcommon -no-feature-gtk3
-qt-zlib -qt-libpng -qt-freetype -qt-harfbuzz -qt-pcre
```

Off, and why: OpenGL and Vulkan (no GPU), xcb and GTK (no X server), D-Bus and GLib (no session bus or GLib event loop), ICU (Qt's own Unicode tables are enough), fontconfig (fonts are named by directory instead), libinput, libudev, tslib, mtdev and xkbcommon (Qt's built-in evdev handlers and keymap need none of them), inotify (Burrow has none: `QFileSystemWatcher` falls back to polling), SQL, OpenSSL and JPEG (not needed yet). Every library Qt does use is bundled, so the cross build never looks at the host's; `PKG_CONFIG_LIBDIR=/nonexistent` makes sure. Programs build with Qt's own `lib/cmake/Qt6/qt.toolchain.cmake`, and a static program links only the platform plugins its CMake file names (`qt_import_plugins`: linuxfb, and offscreen for host checks).

The environment Qt finds its devices and fonts in, instead of udev and fontconfig (`user/qt/qt.env`, on the Qt disk; the kernel adds it to every program's environment):

```
QT_QPA_PLATFORM=linuxfb:fb=/dev/fb0
QT_QPA_EVDEV_KEYBOARD_PARAMETERS=/dev/input/event0:grab=1
QT_QPA_EVDEV_MOUSE_PARAMETERS=/dev/input/event1
QT_QPA_FONTDIR=/opt/qt/fonts
XDG_RUNTIME_DIR=/tmp
```

What Qt needed from Burrow that nothing else had: `waitid` (QProcess) and `FIONREAD` on pipes (BUG-0016); the census of its system calls on Linux (`make qt-census`, `tests/regression/census-qt.txt`) found everything else already there, but a census counts calls, not ioctl requests. Qt is LGPLv3/GPL: a static program must come with what lets you relink it, which here is Qt's pinned source download and `scripts/qt.sh`.

## X11 (W16)

The desktop is an X server and its clients built static for musl: `scripts/x11.sh` (`make x11`) fetches pinned sources once into `~/.cache/burrow-x11`, builds each library into a sysroot there with the ports' musl cross compiler, and puts the programs into `build/x11-root`, the tree that becomes the X disk at `/opt/x11` ([DESKTOP.md](DESKTOP.md) is how to use it). The libraries, in build order: zlib 1.3.1, expat 2.6.4, FreeType 2.13.3, xorgproto 2024.1, xtrans 1.5.2, libXau 1.0.12, libXdmcp 1.1.5, libpthread-stubs 0.5, xcb-proto and libxcb 1.17.0, libX11 1.8.12, libXext 1.3.6, libXrender 0.9.12, libXinerama 1.1.5, fontconfig 2.15.0, libXft 2.3.9, and for the server libfontenc 1.1.8 and libXfont 1.5.4 (version 1: TinyX predates libXfont2). Then TinyX (`Xfbdev`, a 2024 commit of tinycorelinux/tinyx), dwm 6.5, st 0.9.2 and dmenu 5.3 with `user/x11/*-config.h`, and Doom's engine with an X backend of ours (`scripts/x11-doom.sh`).

Every autotools package is configured the same way, so nothing probes the host's libraries:

```
--host=x86_64-linux-musl --prefix=<sysroot> --disable-shared --enable-static
CC=x86_64-linux-musl-gcc PKG_CONFIG_LIBDIR=<sysroot>/lib/pkgconfig:<sysroot>/share/pkgconfig PKG_CONFIG_PATH=
CC_FOR_BUILD=gcc            (libX11's makekeys runs on the host; xcb-proto needs the host's python3)
```

Beyond that, per package: `--enable-malloc0returnsnull` for libX11, libXext, libXrender and libXinerama (it cannot be probed when cross compiling); FreeType with zlib only (`--with-png=no --with-bzip2=no --with-harfbuzz=no --with-brotli=no`); libX11 `--disable-xf86bigfont`; libXfont `--disable-fc --disable-snfformat --enable-freetype`; fontconfig `--sysconfdir=/opt/x11/etc --with-default-fonts=/opt/x11/fonts/ttf --with-cache-dir=/tmp/fontconfig --disable-cache-build`, installed into a staging tree because its configuration belongs on the X disk, not in the sysroot. TinyX is `autoreconf`ed in a copy, patched (`user/x11/patches/`, each patch says why) and configured `--disable-xvesa --enable-xfbdev --disable-xdmcp --disable-xdm-auth-1 --with-default-font-path=/opt/x11/fonts/misc`, then linked `-all-static`. The suckless programs use their own Makefiles with `LIBS="-static <the X libraries>"`; static linking needs the whole chain spelled out: `-lXft -lfontconfig -lexpat -lfreetype -lz -lXrender -lXinerama -lXext -lX11 -lxcb -lXau -lXdmcp`.

Fonts: the server needs the core fonts `fixed` and `cursor`, made from font-misc-misc and font-cursor-misc's BDF files with the host's `bdftopcf` and `mkfontdir`; their aliases are rewritten from ISO 8859-1 to ISO 10646-1 names, because upstream derives the 8859 sets with `ucs2any` and the build does not. Xft's fonts are DejaVu (W15's download), found through `user/x11/fonts.conf`. Xlib's locale database goes on the disk too (`XLOCALEDIR`): without it `XOpenIM` fails, which dmenu does not survive, and UTF-8 window titles cannot be set.

Off, and why: shared libraries (Burrow has no dynamic loader), XKB (TinyX has none; doomgeneric's own Xlib backend needs it, so Doom has ours), GLX and Xvideo (not in TinyX's build), while MIT-SHM, which TinyX builds, works since W18 (Burrow's SysV shared memory; Doom's X backend uses it, linking `-lXext`), xvesa (no VESA BIOS calls in a 64-bit kernel), XDMCP and its authentication (no network, no display manager), fontconfig's cache build (it would run the target's `fc-cache` on the host), and the libraries' documentation.

The environment (`user/x11/x11.env`, on the X disk; the kernel adds it to every program's environment, replacing a default of the same name):

```
DISPLAY=:0
PATH=/opt/x11/bin:/bin
XLOCALEDIR=/opt/x11/share/X11/locale
FONTCONFIG_FILE=/opt/x11/etc/fonts/fonts.conf
```

What X needed from Burrow: interval timers and `SIGALRM` (the server's scheduler), `KDGKBENT` (the server reads the keymap from the console), a mousedev-exact `/dev/input/mice` (BUG-0018), `statfs`, `fadvise64`, `mremap` and `link` answering `-EPERM` (fontconfig), pseudo-terminals with controlling terminals (st), ioctl requests read as 32 bits (BUG-0019), and `#!` scripts in `execve` (dmenu_run). `make x11-census` runs the clients against the host's Xvfb under strace and compares their calls with `tests/regression/census-x11.txt`; the server cannot run on WSL's console, so Burrow's own unimplemented-call log is its census. Licences: the X libraries are MIT-style, TinyX's changes GPLv3 on an MIT base, dwm, st and dmenu MIT/X; the pinned downloads and the scripts are what lets you rebuild them.
