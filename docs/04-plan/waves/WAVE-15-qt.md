# W15 Qt on the Framebuffer  (M15)  — status: planned

Goal: run Qt widget programs on Burrow, drawn straight onto `/dev/fb0` with Qt's `linuxfb` platform and driven by the keyboard and mouse through evdev, with no X server. Demo: `make run-qt` boots to `burrow>`; `run /opt/qt/bin/qtpad /home/hello.c` opens a Qt text editor; click into it, type, save with Ctrl-S, quit, and `tcc` builds the edited file. Stories: E15 ([BACKLOG.md](../BACKLOG.md)).

Where we start (after W14): a mouse on `/dev/input/event1`, the evdev classification ioctls, `select`, Unix `socketpair`, file `mmap`, `getrandom` and `/dev/urandom`, `statx`, a real-time clock, the `KD*`/`VT_*` terminal calls, and 512 MiB of RAM. C++ with exceptions and `std::thread` already runs (W12, the musl.cc GCC 11.2.1 toolchain).

Design (continues W11-W12: download pinned sources, build static on the host, prove on Linux, then on Burrow):
- **qtbase only, static, cross-built.** Qt Core, Gui and Widgets from the `qtbase` source of the newest Qt 6 release at download time, sha256-pinned in `scripts/qt.sh` (the download needs a go-ahead). Qt 6 cross-compiles against a host build of the same version (`QT_HOST_PATH` for `moc`, `rcc`, `uic`), so the script builds qtbase twice: once for the host (tools only), once for Burrow with the musl cross toolchain. Configure keeps it small and free of what Burrow lacks: `-static -release -qpa linuxfb -no-opengl -no-xcb -no-libinput -no-libudev -no-glib -no-icu -no-fontconfig -no-dbus -no-feature-inotify`, with Qt's bundled zlib, libpng, FreeType, HarfBuzz and PCRE2. The build needs `cmake` (the host has 3.31) and `ninja` (`sudo apt install ninja-build`: not installed yet).
- **Fonts come with us.** Without fontconfig Qt reads every font in `QT_QPA_FONTDIR`; DejaVu Sans and DejaVu Sans Mono (free licence; a second pinned download) go into `/opt/qt/fonts`.
- **Qt lives on a data disk.** A static Qt program is 15-30 MiB. Like Doom's WAD (W11), the programs, fonts and a `qt.env` go on a FAT32 image (`qt-data.img`, label `QTDATA`, mounted at `/opt/qt`) instead of the initrd, which the loader reads with PIO at every boot and keeps in RAM. Running programs from a FAT mount is new and is checked in S02.
- **Environment, not patches.** `QT_QPA_PLATFORM=linuxfb:fb=/dev/fb0`, `QT_QPA_EVDEV_KEYBOARD_PARAMETERS=/dev/input/event0:grab=1`, `QT_QPA_EVDEV_MOUSE_PARAMETERS=/dev/input/event1`, `QT_QPA_FONTDIR=/opt/qt/fonts`, `XDG_RUNTIME_DIR=/tmp/run`. Qt finds its devices from these (no udev); any patch to Qt goes in `user/qt/patches/` with a note saying why.
- **Licence.** qtbase is LGPLv3/GPL. Burrow ships Qt's source download and the build script that links it, which is what static linking under the LGPL asks for; `docs/06-guides/PORTING.md` says so.

### [ ] W15-S01 Qt on Linux first (E15)
`scripts/qt.sh`: download and pin qtbase (and the fonts), build the host tools and the static musl qtbase into `$QT_CACHE` (default `~/.cache/burrow-qt`), and build `user/qt/qthello` (a `QCoreApplication` that prints, starts a `QThread`, runs a `QTimer`, reads a file and exits) and `user/qt/qtwindow` (a `QRasterWindow` painting rectangles and text). Both run on the host first: `qthello` as is, `qtwindow` with `QT_QPA_PLATFORM=offscreen`, saving a PNG. A census, like W6's for Go: `strace -f` of both on Linux lists every syscall Qt makes, kept as `tests/regression/census-qt.txt`, and every number in it is in POSIX_COVERAGE.md (or the slice that adds it is named). `make qt` builds it all; it is not in `make test` (an hour of build time on first run).

### [ ] W15-S02 QtCore on Burrow (E15)
`qthello` runs on `go-qt.img` from `/opt/qt/bin`: signals and slots across threads, timers, `QFile`, `QDir`, `QProcess` (fork and exec of `/bin/echo`), `QRandomGenerator`, `QDateTime::currentDateTime` (W14's RTC), the event loop on `eventfd` and `ppoll`. Each missing call the census missed is fixed here; `[qt] core ok` is a smoke marker. Running programs from a FAT mount (execve through the VFS, pages from W14-S05's file mappings) is tested here.

### [ ] W15-S03 Pixels: QtGui on /dev/fb0 (E15)
`qtwindow` on Burrow through `linuxfb`: Qt opens `/dev/fb0`, reads the mode with `FBIOGET_VSCREENINFO`/`FBIOGET_FSCREENINFO`, maps it, takes the console with `KDSETMODE`, and paints; text is drawn with FreeType from `/opt/qt/fonts`. A screen check samples the rectangles and a pixel inside a glyph; the console comes back when the program exits (and when it is killed).

### [ ] W15-S04 Input: keyboard and mouse into widgets (E15)
`user/qt/qtdemo`: a `QMainWindow` with a line edit, buttons, a list and a menu. Qt's evdev handlers read `event0` (keyboard, grabbed, Qt's built-in keymap: no xkbcommon) and `event1` (mouse, with Qt's drawn cursor). The smoke test types into the line edit, moves the mouse with W14's `mouse` steps, clicks a button that appends the text to the list, opens the menu, and quits with a keyboard shortcut; serial markers from the program and screen checks prove each step.

### [ ] W15-S05 Fast enough (E15)
Measure: start-up time to the first frame, time per full-screen repaint, memory in use, syscalls per second while idle and while dragging the mouse. Set budgets (for example, first frame within 3 s and an idle program making no syscalls) and fix the hot spots; likely candidates are reading fonts at start-up (file mappings vs reads), copying through the syscall ring, and Qt's backing store flush to `/dev/fb0` (full frame vs damaged rectangles). `make qt-bench` (not in `make test`, like `doom-bench`) fails when a budget is missed.

### [ ] W15-S06 qtpad, and make run-qt (E15)
`user/qt/qtpad`: a small editor on `QPlainTextEdit` (open, save, save as, find, a monospace font, line numbers, C and Lua highlighting with `QSyntaxHighlighter`) that edits files in `/home`. `make run-qt` boots `go-qt.img` with the home disk and the Qt disk in a QEMU window; `make qt-smoke` opens `/home/hello.c` in `qtpad`, adds a line, saves, quits, and compiles the file with `tcc`; the second boot of `dev-smoke` style finds it. WORKBENCH.md gets a section on Qt.

Exit: **M15 demo — Qt on Burrow.** `make qt-smoke` joins `make test` (skipping without `make qt`, like `doom-smoke`); `make clean test` green; PORTING.md gains a Qt section (configure line, environment, what is switched off and why); DEVLOG wave entry.

Risks and pitfalls (PIT-NN entries go into BUGS_AND_MITIGATIONS.md when the wave starts):
- **Build cost.** A static qtbase is a long build and about 2 GiB on disk; S01 builds once into a cache and never inside `make test`. Two builds of the same source (host and target) must use the same configure feature set where `moc` output depends on it.
- **Qt's cross-build assumes a sysroot with pkg-config.** Everything Qt would look up is switched off or bundled, so the sysroot is just the musl toolchain's; if configure still probes the host's libraries, a toolchain file pins it.
- **The census is not complete.** strace on Linux shows the calls Qt makes there; Burrow's answers differ (no `/proc`, no udev, no symbolic links), so some fallback paths only show on Burrow. The `unimplemented syscall` log catches them.
- **Memory.** A Qt program with fonts and the backing store (3 MiB per 1024x768 frame) runs in tens of MiB, and fork copies eagerly (`QProcess`). 512 MiB from W14-S01 should be plenty; copy-on-write fork stays in the backlog.
- **The newest Qt may want a newer compiler** than GCC 11.2.1. If so, S01 pins the newest Qt 6 release the toolchain builds and notes why.
