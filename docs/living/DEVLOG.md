# DEVLOG

Newest entries at the top. One entry per working session, plus a summary per wave. Write what you did, what surprised you, and what you would tell your past self. Link BUG and ADR ids.

Template:
```text
## YYYY-MM-DD — W?-S?? short title
Did:       ...
Surprise:  ...
Learned:   ...
Next:      ...
Links:     BUG-xxxx, ADR-xxxx
```

---

## 2026-11-03 — W16-S02 Xfbdev on Burrow

**Did:** TinyX's `Xfbdev` runs on Burrow from the X disk at `/opt/x11` and serves `xhello`: a window with rectangles, the core font and Xft text, a mouse click and a key, then the console comes back when the server is killed (`make x11-smoke`, now in `make test`). New in the kernel: interval timers and `SIGALRM`, `KDGKBENT` with a US keymap, `statfs`/`fstatfs`, `fadvise64`, `mremap`, and `link` answering `-EPERM`.

**Surprise:** the server itself asked for little; the trouble was all in the edges. The core font `fixed` could not open because the aliases named fonts our build never made, and the mouse showed BUG-0018: W14's `/dev/input/mice` answered PS/2 commands in a way no test that writes everything at once could catch, so TinyX's handshake first stalled and then lost packet alignment. Reading TinyX's `mouse.c` beside Linux's `mousedev.c` found both halves.

**Next:** W16-S03, st on a pseudo-terminal.

Links: BUG-0018

## 2026-11-03 — W16-S01 the X stack on Linux first

**Did:** `make x11` fetches 26 pinned sources and builds the X libraries (Xlib, xcb, Xft, fontconfig, FreeType, libXfont 1), the core fonts, TinyX's `Xfbdev`, dwm, st and dmenu, all static for musl: 17 MB for the whole desktop, against 82 MB for five Qt programs. On the host, against Xvfb and driven by xdotool, a new `xhello` draws with the core font and Xft and takes a key, st runs busybox `sh`, dmenu returns a chosen line, and dwm manages a window and quits. The census has 48 calls; Burrow lacks three (`fadvise64`, `fstatfs`, `mremap`), named for S02.

**Surprise:** that 18 autotools cross builds went through at the first attempt, given `PKG_CONFIG_LIBDIR`; TinyX needed only the xtrans m4 path and one header patch (`XTest.h` left the protocol headers years ago). The test harness taught two small things: `xdotool windowactivate` needs a window manager, and st's exit status when its shell ends is a race inside st, so the test checks what st did instead.

**Next:** W16-S02, `Xfbdev` on Burrow.

## 2026-11-03 — W15 done: Qt on the framebuffer (M15)

**Did:** the wave in six slices. Qt 6.12 built static for musl, with a census of its system calls on Linux (S01); QtCore on Burrow from a FAT disk at `/opt/qt`, which needed `waitid` and `FIONREAD` on pipes (S02, BUG-0016); Qt painting on `/dev/fb0` through linuxfb (S03) and Qt widgets driven by the keyboard and mouse through evdev (S04), both with no kernel change; budgets for start-up, repaint, memory and idling, which found disk reads five times too slow everywhere (S05, BUG-0017); and `qtpad`, an editor for `/home`, with the ATA driver's second channel so the home and Qt disks can be attached together (S06). The M15 demo is in `make qt-smoke`: qtpad adds a line to `/home/hello.c`, saves, quits, `tcc` builds the file, and a second boot finds the line.

**Surprise:** how much of it was already there. Qt asked for 37 distinct system calls and Burrow had 36; the pixels and the input needed no kernel change at all, because W11's fbdev and W14's evdev, mouse and terminal modes speak Linux closely enough. The work went into the test harness (NUL bytes in logs, QEMU's small mouse queue, widget coordinates) and into a disk path that had been slow since W8 without anyone timing it.

**Next:** E16 (an X server, dwm, GTK) or E12 (hardening); see BACKLOG.md.

## 2026-11-03 — W15-S06 qtpad and make run-qt

**Did:** the ATA driver's secondary channel (`hdc`, `hdd`) and mounting every data disk by its label; `qtpad`, a Qt editor (line numbers, C and Lua colouring, save through `QSaveFile`, find, save as); `make run-qt`; and `qtpad-smoke` in `make qt-smoke`: edit `/home/hello.c`, save, quit, build with `tcc`, reboot, still there, `fsck.fat` clean.

**Surprise:** writing the secondary channel's probe showed that its port wrapper had hidden S05's `rep insw` all along; with it the first Qt frame came in 1.8 s instead of 2.8 s, and BUG-0017's entry now says so. Five static Qt programs are 82 MB, which filled the 64 MiB Qt disk.

**Next:** see the wave entry above.

## 2026-11-03 — W15-S05 Qt fast enough

**Did:** `qtbench`, started through a small `stopwatch` that marks the `execve`, measures the time to the first full-screen frame, a full-screen repaint, the memory used and how often an idle Qt wakes; `make qt-bench` checks them against budgets. The first numbers were fine except one: 11.6 s to the first frame, almost all of it before `main`.

**Surprise:** BUG-0017, and where it was not. Reading the 15 MB program took 10.5 s; string port I/O (`rep insw` for a whole sector) changed nothing at all, and reading runs of FAT clusters at once saved only 0.4 s. The cost was per request: READ SECTORS gives one sector per data request and QEMU fetches each one separately. With READ MULTIPLE (16 sectors per request) the first frame arrives in 2.8 s, and every disk read in Burrow is about five times faster. Repaints take 11 ms, the program 6.5 MiB, and an idle Qt program never wakes.

**Next:** W15-S06, `qtpad` and `make run-qt`.

## 2026-11-03 — W15-S04 keyboard and mouse into Qt widgets

**Did:** `qtdemo`, a Qt widgets window (a menu bar, a line edit, an Add button, a list), driven by the PS/2 keyboard and mouse through Qt's own evdev handlers and Qt's drawn cursor. The smoke test types into the line edit, clicks Add, presses Enter for a second line, opens and closes the File menu, and quits with Ctrl+Q; serial markers and screen checks prove each step.

**Surprise:** the kernel needed nothing; the test needed three things. QEMU's PS/2 queue holds only a few packets, and motion that does not fit is kept until the next input event and merged into it, so a big move towards the corner swallowed the move back out (`sendkeys.py` now splits moves). Qt's central widget starts below the menu bar, so fixed widget positions are not screen positions (`qtdemo` prints where its widgets are). And a clicked button takes the keyboard focus, so the next typing went into the button.

**Next:** W15-S05, measuring and fixing Qt's speed on Burrow.

## 2026-11-03 — W15-S03 Qt draws on /dev/fb0

**Did:** `qtwindow` on Burrow through Qt's linuxfb platform: it opens `/dev/fb0`, reads the mode, maps it, paints three rectangles and a line of DejaVu Sans from FreeType, and the console returns when it quits, and when it aborts with the screen taken. `qt.screen` checks 14 pixels, one inside a glyph.

**Surprise:** that Burrow needed nothing: W11's fbdev and W14's terminal modes were enough, and the pixels matched the host's offscreen PNG exactly. The only failure was the harness's: linuxfb writes its console escape sequences with a trailing NUL byte, `grep` then called the serial log a binary file, and every marker went "missing". `smoke.sh` now reads logs with `grep -a`.

**Next:** W15-S04, keyboard and mouse into widgets.

## 2026-11-03 — W15-S02 QtCore on Burrow

**Did:** a Qt data disk: `build/qt-root` as FAT32 with label `QTDATA`, which the kernel mounts at `/opt/qt`, adding its `qt.env` to every program's environment. `qthello`, a 7.6 MB static-PIE Qt program, runs from that FAT mount: signals across a `QThread`, timers on the event loop (eventfd and ppoll), `QFile`, `QDir`, `QRandomGenerator`, the RTC's date, and a `QProcess` that forks and execs `/bin/echo` and reads its output. `make qt-smoke` checks it and joins `make test` (it skips without `make qt`). Burrow gained `waitid`, which QProcess reaps with.

**Surprise:** BUG-0016. Every check passed on the first boot except QProcess, which got "h" from a child that wrote a whole line: Qt sizes its pipe reads with `ioctl(FIONREAD)`, Burrow's pipes answered `ENOTTY`, and Qt then reads one byte. Nothing was logged, because `ioctl` is implemented; the census counts system calls, and an ioctl request is invisible to it.

**Next:** W15-S03, `qtwindow` on `/dev/fb0` through linuxfb.

## 2026-11-03 — W15-S01 Qt on Linux first

**Did:** `scripts/qt.sh` (`make qt`) fetches qtbase 6.12.0, its security diff and the DejaVu fonts (all sha256-pinned), builds Qt for the host (the tools a cross build runs) and then statically for Burrow with the musl toolchain the ports already use: Widgets, linuxfb and evdev, with FreeType, HarfBuzz, PCRE2 and libpng bundled and fontconfig, udev, libinput, D-Bus, OpenGL and ICU switched off. `user/qt` has `qthello` (QtCore: moc, a queued signal across a `QThread`, timers, `QFile`) and `qtwindow` (a `QRasterWindow` with rectangles and DejaVu text), both static-PIE; both run on Linux, `qtwindow` through the offscreen platform into a PNG. The census, like W6's for Go, ran both under strace.

**Surprise:** how little Qt asks for: 33 distinct system calls, and every one already had a handler, mostly thanks to W14 (`eventfd2`, `ppoll`, `getrandom`, `readlink`). The census cannot see one call: musl reads `clock_gettime` through the vDSO, which Burrow lacks, so it is listed by hand. Building our programs needed Qt's own `qt.toolchain.cmake`: with ours, `find_package` looked only inside the musl sysroot.

**Next:** W15-S02, `qthello` on Burrow from a FAT data disk at `/opt/qt`.

## 2026-11-03 — W14 done: desktop groundwork (M14)

**Did:** the wave in eight slices: 512 MiB images and physmap above 4 GiB with an out-of-memory kill (S01, with BUG-0014: every system call took 2 ms); a PS/2 mouse as `/dev/input/event1` and `/dev/input/mice` (S02); `select`, `getrandom` and the start-up calls (S03); a real-time clock that made every date real (S04); `mmap` of files (S05); Unix socket pairs (S06, with BUG-0015: pipes never raised `SIGPIPE`), then names, `connect`/`accept` and `SCM_RIGHTS` (S07); and the console's `KD*`/`VT_*` modes (S08). The M14 demo, `make desk-smoke`, runs the test programs and `paint`: it takes the screen with `KD_GRAPHICS` and `K_OFF`, gets its colours from a forked palette process over `/tmp/paint.sock`, paints a stroke the smoke test drags with the mouse, and gives the screen back; a second `paint` crashes with the screen taken and the kernel gives it back by itself. Four test programs (`startup`, `socktest`, `socknames`, and `mmaptest` in part) run on the host's Linux before Burrow, so their checks are Linux's answers, not mine.

**Surprise:** in S08, nothing: `desk-smoke` passed on its first run, pixels included. Across the wave the surprises were the two bugs and how often matching Linux exactly meant an answer I would not have guessed (`EOPNOTSUPP` for `socketpair(AF_INET)`, `SOCK_RAW` as `SOCK_DGRAM`, `EACCES` before `ECONNREFUSED`). Left for later: named datagram sockets (E14.F08), an absolute pointer so QEMU needs no grab, and a garbage collector for descriptors sent around in circles (Linux has one; Burrow accepts the leak).

**Next:** W15, Qt on `/dev/fb0` (`docs/04-plan/waves/WAVE-15-qt.md`); it needs downloads (qtbase, DejaVu fonts) and `ninja-build` on the host.

## 2026-11-03 — W14-S08 graphics programs take the screen

**Did:** `kernel/vt` (the keyboard, display and VT modes, each undone when its process exits), the `KD*`/`VT_*` ioctls on the terminal and `/dev/tty0`/`tty1`, scancodes routed by the keyboard mode, `KD_GRAPHICS` standing the console aside like `/dev/fb0`, and the colour-map and blank ioctls on `/dev/fb0`. `paint` and `make desk-smoke` are the wave's demo.

**Surprise:** none; the one change of plan was making `paint`'s colours really arrive over a Unix socket, as the roadmap promised, rather than from a table.

**Next:** W15.

## 2026-11-03 — W14-S07 named sockets and descriptor passing

**Did:** `socket`, `bind`, `listen`, `connect`, `accept`/`accept4`, `getsockname`, `getpeername` for Unix stream and seqpacket sockets, over path names (a socket node in `/tmp`, through a new `vfs.MakeSocket`) and abstract names (a table in the kernel), with autobind, `SO_PEERCRED`, `SO_PASSCRED` and `SO_ACCEPTCONN`. `sendmsg` and `recvmsg` carry `SCM_RIGHTS` (new descriptors for the same open files, `MSG_CTRUNC` when the buffer is short) and `SCM_CREDENTIALS`. `socknames` runs a forked client and server over both kinds of name and passes a pipe end; its 23 checks pass on the host's Linux and on Burrow.

**Surprise:** on the first Burrow boot only `credentials` failed: a plain `write` on a socket queued its bytes with sender pid 0, because the file layer's `WriteCancel` does not know who is writing. `Thread.write` now hands sockets the caller's pid. On Linux the same program first failed `path-file`: connecting to `/etc/hostname` as a normal user is `EACCES` (Linux checks write permission before the socket type), so the check uses a file of its own. Named datagram sockets were left out (`-EOPNOTSUPP`, backlog E14.F08).

**Next:** W14-S08, graphics programs take the screen (`KD*`/`VT_*`).

## 2026-11-03 — W14-S06 Unix socket pairs

**Did:** `socketpair(AF_UNIX)` for stream, datagram and seqpacket sockets, with `send`/`recv`, `sendmsg`/`recvmsg`, `shutdown`, the `MSG_` flags, `SO_TYPE`/`SO_ERROR`/the buffer sizes, `FIONREAD` and `S_IFSOCK`. Each end is a `Canceller` and a `Poller`, the same shape as a pipe end, so read/write, non-blocking mode, `EINTR`, poll, select and epoll worked without a line changed in them. `socktest` makes 29 checks; `make ports-smoke` runs it on the host's Linux and then on Burrow, and it passed on Burrow's first boot.

**Surprise:** pipes had never raised `SIGPIPE` (BUG-0015): a writer with no reader got `EPIPE` and carried on. `Thread.write` now sends the signal, for pipes and sockets alike. Copying Linux exactly also meant two answers I would not have guessed: `socketpair(AF_INET)` is `EOPNOTSUPP`, and `AF_UNIX` quietly turns `SOCK_RAW` into `SOCK_DGRAM`.

**Next:** W14-S07, socket names, `listen`/`accept`/`connect`, and `SCM_RIGHTS`.

## 2026-11-03 — W14-S05 mmap of files

**Did:** `mmap` maps regular files privately: an area remembers its file and the offset at its start, and a page fills from the file when it is first touched (a fault, or the kernel copying in or out), with zeros past the end of the file. Splits from `munmap`, `mprotect` and `MAP_FIXED` keep the offsets right; `MADV_DONTNEED` lets the file show through again; fork copies the pages like any private memory. Read-only `MAP_SHARED` is the same thing; writable `MAP_SHARED` is `-ENODEV`, since nothing writes pages back. `mmaptest` maps a file from each file system (memfs, tarfs, FAT) and passed on the first boot.

**Surprise:** none worth a BUG. The one design question, how a mapping keeps its file after `close`, had a short answer: no Burrow file system's `Close` does anything, so the area just keeps the `io.ReaderAt`. That is written down in `area.go` for the day one does.

**Next:** W14-S06, Unix sockets with `socketpair`.

## 2026-11-03 — W14-S04 a real-time clock

**Did:** `drivers/rtc` reads the CMOS clock at boot and hands it to the nucleus (`nu_set_realtime`), which from then on answers the realtime clocks with wall time. Putting the clock in the nucleus rather than in Go's `proc` made the Go runtime's own `time.Now` real too, so memfs, `/tmp` and FAT got real dates without a line changed in them. User programs gained `clock_getres`, `gettimeofday`, `time`, `FUTEX_CLOCK_REALTIME` deadlines, and an `-EPERM` for anyone setting the clock. The busybox and dev smoke tests boot with a fixed RTC (2030-01-02 03:04:05), so `date`, `ls -l` and the FAT dates the host reads with `mdir` can all be checked.

**Surprise:** busybox's `ls -l` printed no date column at all: `FEATURE_LS_TIMESTAMPS` was off in `user/busybox.config`. Two markers on one output line cannot both match, since `smoke.sh` moves on a line after each match; the date check is one marker.

**Next:** W14-S05, `mmap` of files.

## 2026-11-03 — W14-S03 select and the small start-up calls

**Did:** `select` and `pselect6` over the same wait loop as `poll`; `getrandom`, `/dev/random` and `/dev/urandom` from a new `kernel/random` (ChaCha8, seeded at boot from RDRAND, or from the TSC when the CPU has none: not for cryptography); `statx`; `readlink`/`readlinkat` (nothing is a link); `sysinfo` with real RAM figures from a new nucleus call, `nu_mem_info`; `umask`; `getrusage`; `sched_getparam`, `sched_getscheduler`, `sched_setaffinity`; `membarrier`'s query. `user/startup` checks all of them, and `make ports-smoke` runs it on the host's Linux before it boots Burrow, so a check that only Burrow passes cannot get in.

**Surprise:** `pipe2` had honoured `O_NONBLOCK` since W11, though POSIX_COVERAGE.md still said it did not. musl's `sched_getscheduler` and `sched_getparam` return `ENOSYS` without making a system call, so `startup` makes them with `syscall()`. Neither was a bug worth a number.

**Next:** W14-S04, a real-time clock.

## 2026-11-03 — W14-S02 a PS/2 mouse

**Did:** the 8042's second port: the mouse comes up with the IntelliMouse handshake (a wheel, 4-byte packets) on IRQ 12, and its packets become `/dev/input/event1` (Linux input events) and `/dev/input/mice` (mousedev's PS/2 stream). Both event devices answer `EVIOCGBIT`, so a program can tell the keyboard from the mouse the way Qt and X servers do. `mousetest` draws a cursor that follows QEMU's `mouse_move`, and the devices smoke test finds it on screen at the right spot.

**Surprise:** none worth a BUG: every piece worked on its first QEMU boot, after the host tests (a fake IntelliMouse on the fake 8042) had caught the sign conventions. PS/2 y grows upward, Linux's `REL_Y` grows downward, and the wheel byte is negative for up: three places for a sign to go wrong, each pinned by a test.

**Next:** W14-S03, `select` and the small calls start-up code makes.

## 2026-11-03 — W14-S01 more memory, and BUG-0014

**Did:** Go images boot with 512 MiB (`GO_MEM`); `make mem-smoke` touches 1.5 GiB on a 2 GiB guest. The physmap no longer maps the PCI hole as RAM on guests over 3 GiB, and a process that runs out of memory is killed (`SIGKILL`, logged) instead of panicking the nucleus, which now keeps 32 MiB for the kernel.

**Surprise:** the first `memtest` needed 8 s for 16 MiB. Not memory at all: every system call took 2 ms (BUG-0014). The dispatcher blocks in the nucleus holding Go's only P, so each handler waited for sysmon, which sleeps in 1 ms ticks. One `runtime.Gosched()` made calls 24 times faster (84 us). It had been there since W9; Doom made too few calls to notice.

**Learned:** with GOMAXPROCS 1, every blocking syscall is a scheduling decision. A latency test (`syslat`) belongs next to the throughput ones.

**Next:** W14-S02, the PS/2 mouse.

## 2026-11-03 — Waves 14 and 15 planned: desktop groundwork, then Qt

**Did:** looked at what TinyX `Xfbdev`, dwm, GTK and Qt need from the kernel, and planned [W14](../04-plan/waves/WAVE-14-desktop-groundwork.md) and [W15](../04-plan/waves/WAVE-15-qt.md).

**Found:**
- Missing for any of them: Unix sockets (none exist), a mouse (the PS/2 driver drops its bytes), `select`, `mmap` of files, more than 128 MiB, `/dev/urandom` and `getrandom`, a wall clock, `pipe2` with `O_NONBLOCK`, and the `KD*`/`VT_*` calls graphics programs make to take the screen. That is W14.
- Qt does not need X: its `linuxfb` platform draws on `/dev/fb0` and reads evdev itself, so a static qtbase is the shortest way to real widgets. That is W15.
- TinyX (tinycorelinux's kdrive fork; 1.2.0 is its last release with working input) has no XKB and no XInput, which is fine for dwm and st but not for GTK3, whose X backend requires XInput 2. GTK needs the kdrive `Xfbdev` of the 1.20 server branch (removed upstream after it), about 15 libraries, and realistically dynamic linking and copy-on-write fork. Those are E16 in the backlog.

**Next:** W14-S01, more memory.

---

## 2026-11-03 — Wave 13 complete: a workbench (M13)

**Did:** W13-S01..S06. Burrow is a small development machine: `make run-dev` boots busybox, `vi`, kilo, Lua and TinyCC with a home disk that survives a power-off.

```text
burrow> run /bin/busybox sh
# cd /home
# kilo hello.c          (syntax colours on the screen console; Ctrl-S: 469 bytes written on disk)
# tcc -static -o hello hello.c; ./hello 10
fib(10) = 55, computed on Burrow
# exit
burrow> poweroff
   ... boot again on the same disk ...
# cat notes.txt; ./hello 12
saved on the first boot
fib(12) = 144, computed on Burrow
```

- **Editors:** busybox `vi` (a config change and `ftruncate`), and kilo (antirez, BSD-2-Clause) after the TTY learned `VMIN`/`VTIME`.
- **The screen console became a terminal:** a cell grid and a VT100/ANSI subset (`gfx/vt.go`: positioning, erase, insert/delete line, scrolling regions, colours, reverse, the alternate screen, an underline cursor).
- **Writing goes to the disk:** `block.Writer` with ATA `WRITE SECTORS` and `FLUSH CACHE`; writable FAT32 (`fs/fat/write.go`, `dirwrite.go`, `rename.go`) with data-then-entry-then-FAT ordering; rename, fsync and the metadata calls; a FAT volume labelled `BURROWHOME` mounts at `/home`.

**Numbers:** `fsck.fat` accepts what the kernel writes, on Linux-made (4 KiB clusters) and Burrow-made (512 B) images; the two-boot workbench test passes with `fsck.fat` after it; the disk-write boot is checked byte for byte on the host.

**Surprises:**
- Doom's smoke test had raced Doom's screen melt since W11; the two "unexplained" failures then were this. `waitpixel` waits for a screen condition instead of a time (BUG-0013).
- kilo tells a lone Esc from an arrow key with `VMIN=0 VTIME=1`: a read that times out. A terminal that ignores `VTIME` makes Esc wait for the next key.
- vi exits without a newline, so its last status line and the next prompt share a serial line; smoke markers are per line.
- Twice I committed in parallel with the checks and the checks failed (an unrelated Doom race, then my own interop step that could not rerun). Committing only after the suite reports is the rule again.

**Learned:** The pieces for "a computer you can work on" were mostly there; what was missing were contracts at the edges: a terminal that is a terminal (escape codes, `VMIN`/`VTIME`), and a disk that takes writes in an order a crash cannot hurt.

**Next:** BACKLOG E12 (SMP, NX everywhere, UEFI, virtio, networking, real hardware); smaller: `make run-dev` with more than one home, copy-on-write fork, a page cache.

**Links:** BUG-0013, WORKBENCH.md, PORTING.md

## 2026-11-03 — Wave 12 complete: ports (M12)

**Did:** W12-S04..S07 on top of S01-S03's threads, epoll and Go. Downloaded, pinned and built by `make ports`, run in `make ports-dl-smoke`:

```text
burrow> run /bin/lua
Lua 5.4.9  Copyright (C) 1994-2026 Lua.org, PUC-Rio
> print(_VERSION .. ' says ' .. 6*7)
Lua 5.4 says 42
burrow> run /bin/cxxtest
cxxtest: exception caught: "thrown 5 frames down", 0 objects left
cxxtest: 4 std::threads summed 1..4000: 8002000
cxxtest: ok
burrow> sh
$ echo 'int main(void) { puts("typed, compiled and run on Burrow"); return 7; }' >> /tmp/t.c
$ tcc -static -o /tmp/t /tmp/t.c; /tmp/t
typed, compiled and run on Burrow
$ echo status $?
status 7
```

Lua 5.4.9, C++ through musl.cc's GCC 11 toolchain (static libstdc++), and TinyCC with a musl sysroot in `/usr` needed **no kernel change at all**: every call they make was already there. `docs/06-guides/PORTING.md` is the recipe and the record of what each port needed.

**Surprises:**
- TinyCC 0.9.27's output crashed on Burrow at a garbage address. The same binary crashed at the same address on Linux: the 2017 release mislinks objects from a 2021 GCC (`R_X86_64_PLT32` calls). Running the binary on Linux first took one command and settled whose bug it was. A development snapshot of TinyCC works.
- `tcc -run` cannot work in a static `tcc` (on Linux either): the program's copy of libc is never initialised.
- Our own shell expands `$?` when it reads a line, so `prog; echo $?` shows the status from before the line. The test asks on the next line; the shell keeps its simplicity.

**Learned:** By the last wave, porting was building, not kernel work: four ports in a row needed nothing new. The Linux ABI decision (ADR-0006) and "implement the contract" paid for everything after W10.

**Next:** all twelve waves are done. What is left is BACKLOG E12 (hardening: SMP, SMEP/SMAP/NX, UEFI, virtio, networking, real hardware), plus debts noted along the way: copy-on-write fork and a page cache for writable files.

**Links:** ADR-0006, PORTING.md

## 2026-11-03 — W12-S01..S03: threads, epoll, a Go user program

**Did:** processes have threads, and a stock Go program runs as a Burrow user process:

```text
burrow> run /bin/threads
threads: counter 80000 (want 80000)
threads: consumer summed 1275 (want 1275)
threads: SIGUSR1 handled by worker 7 (want 7)
threads: ok
burrow> run /bin/gohello
gohello: go1.27.3 on linux/amd64, pid 10
gohello: 8 goroutines squared 1..100: 338350 (want 338350)
gohello: 50 goroutines counted 50000 (want 50000)
gohello: timers: slept and ticked for 76ms
gohello: GC: 9 cycles, 25 MiB allocated in all
gohello: ok
```

All policy, no nucleus change: the nucleus already ran many ring-3 threads per address space. `kernel/proc` gained a `Thread` (TID, signal mask and pending set, EINTR channel, TLS, sigaltstack, `clear_child_tid`), thread-shaped `clone`, `exit` vs `exit_group`, futexes as goroutines on channels, per-thread and process-directed signals with `SA_ONSTACK`, `epoll` and `eventfd` over the `Poller` interface from W10, `mprotect`, `madvise`, rlimits, and demand-zero `mmap`/`brk`.

**Surprises:**
- The Go runtime's first act failed: "failed to reserve page summary memory". It reserves hundreds of MiB it never touches, and Burrow backed every mmapped page at once. The fix is the user-space twin of BUG-0012: areas filled on first touch, through the resumable faults W10 built for copy-on-write.
- musl's `pthread_create` maps a thread stack `PROT_NONE` and opens it with `mprotect`, which did not exist.
- Making every handler take a thread would have touched 70 functions. A second table for the 25 calls that need the caller kept the change honest and small.
- A thread's request queued just before `exit_group` showed up as "request from unknown thread". Dropping it is right; logging an error was not.

**Learned:** The Go runtime is the best conformance test Burrow has: one program exercises threads, futexes, tgkill, alternate signal stacks, epoll, eventfd and lazy memory, and it fails loudly and precisely when any of them is wrong.

**Next:** W12-S04..S07: Lua, C++ with exceptions and `std::thread`, TinyCC on Burrow (each needs a download), and the porting guide.

**Links:** BUG-0009, BUG-0012, ADR-0006

## 2026-11-03 — Wave 11 complete: DOOM (M11)

**Did:** W11-S01..S06. An unmodified static musl build of doomgeneric runs on Burrow and plays Freedoom: Phase 1:

```text
burrow> run /bin/doom -iwad /mnt/freedoom1.wad
 adding /mnt/freedoom1.wad
                           Freedoom: Phase 1
R_Init: Init DOOM refresh daemon - .......................................
I_InitGraphics: framebuffer: x_res: 640, y_res: 400, x_virtual: 640, y_virtual: 400, bpp: 32
I_InitGraphics: Auto-scaling factor: 2
(title screen, New Game, E1M1, Quit Game)
burrow> run /bin/doom -iwad /mnt/freedoom1.wad -timedemo demo1
timed 7117 gametics in 2484 realtics (100.279793 fps)
```

Devices (Go policy, nothing new in the nucleus): `/dev/fb0` (`kernel/fbdev`: the fbdev ioctls, `mmap(MAP_SHARED)` of the framebuffer, the console stepping aside while it is open) and `/dev/input/event0` (`kernel/evdev`: PS/2 scancodes as Linux input events, a queue per open, the keyboard grabbed while open). In `kernel/proc`: `Ioctler`, `Mappable` and `Poller` looked up on a file's device, device pages that are never freed and are shared by fork, real `O_NONBLOCK`, and `readv`. Doom: `make doom` fetches doomgeneric (pinned commit) and Freedoom 0.13.0 (sha256 from its signed checksum file), builds the engine with our backend (`user/doom/doomgeneric_burrow.c`, Linux interfaces only) and puts the 28.8 MB WAD on a FAT32 disk.

**Numbers:** 100-104 fps in `timedemo demo1` under QEMU TCG (Doom's game runs at 35 tics/s), with every frame copied by the backend and every syscall going through the Go dispatcher; RISKS R-07 retired. The Go kernel needed one new syscall for Doom (`readv`).

**Surprises:**
- Doom ran on its second boot. The first failed at `W_Init` because musl's `fread` uses `readv`; nothing else was missing, because W9-W10 had already implemented the Linux behaviour programs rely on.
- `/mnt` is a Windows-backed checkout in WSL: copying `/usr/include/linux` fails on headers whose names differ only in case (`xt_CONNMARK.h`, `xt_connmark.h`). `build/uapi` links to the directories instead.
- The console showed "killed by signal 127" for Doom's `exit(255)`: a status above 128 is ambiguous, so `run` asks the process how it ended.
- `open` stats the path first, which opens and closes the device once: the console repainted itself in the middle of Doom's start-up until taking the screen also cleared it.

**Learned:** Copying Linux's device interfaces paid for itself. The backend written against Linux headers as a planning snippet ran with no code changes; the work was in making each device behave the way the headers promise, and testing that on the host.

**Next:** W12, ports: Lua, C++, a Go user program.

**Links:** ADR-0006, BUG-0004, PIT-27, R-07

## 2026-11-03 — W10-S08 busybox

**Did:** `make busybox` fetches busybox 1.36.1 (the newest release busybox.net marks stable) once, checks the pinned sha256 and builds a 320 KiB static musl binary from `user/busybox.config`: ash with line editing and job control, about 60 applets, run without links in `/bin` (`FEATURE_SH_STANDALONE`, exec path `/bin/busybox`). `make busybox-smoke` boots `go-busybox.img` and types a session: `$((6*7))`, `ls / | wc -l`, `seq 5 | sort -r | head -n 2`, `cd /etc && pwd && cat motd | grep -c .`, a `for` loop, `^C` on a foreground `sleep` (status 130), `exit`. It skips when no busybox build is present, so `make test` and CI work without the download.

**Surprise:** it nearly worked first time. The first boot logged two unknown calls: `prctl` (the name) and `poll`, which ash's line editor calls before every key. Our console forwards keys only while a program reads, so a program that polls the terminal has to count as a reader too, or it would wait forever. Writing the `poll` test also showed that we restarted every EINTR under `SA_RESTART`; Linux never restarts `poll`, `pause`, `nanosleep` or `rt_sigsuspend`.

**Learned:** A smoke wait that matches the echo of the command line is no wait at all: print something the typed text does not contain (`echo slee''ping`).

**Links:** E09.F05

## 2026-11-03 — Wave 10 complete: a Unix (M10)

**Did:** W10-S01..S07. The console starts a shell, and the shell behaves like a Unix shell:

```text
burrow> sh
$ ls / | wc -l
5
$ echo hi > /tmp/x; cat /tmp/x
hi
$ cd /etc; pwd; ls
/etc
hostname
motd
$ spin
spin: spinning until ^C
^C
$ echo $?
130
$ exit
burrow> ps
  PID  PPID  PGID STATE    COMMAND
    1     0     1 kernel   init
```

Nucleus (mechanism): user syscalls save a whole `trap_frame` and return with IRETQ. Go reads and writes a stopped thread's registers (`nu_uthread_regs`, sanitized), starts a thread from a register image (fork), moves a thread to a new address space (execve), and stops a busy thread at its next return to ring 3 (`nu_uthread_interrupt`). Answered faults and interrupt stops resume the thread. Go (policy): a process tree with zombies, `wait4` and init at PID 1; descriptors (dup, pipes, cloexec, fcntl), a cwd and the `*at` calls, writable `/tmp`; signals (handlers, masks, Linux `rt_sigframe`, `SA_RESTART`, `EINTR`, `SIGCHLD`, faults as signals); `kernel/tty` (canonical and raw modes, termios, `^C`/`^\` to the foreground group, setpgid/setsid). Userland: `user/sh.c` (pipelines, redirects, `;`, `$?`, cd/pwd/exit, one process group per job) and `ls cat echo wc kill sleep spin`.

**Numbers:** 66 Linux calls in `POSIX_COVERAGE.md`, 45 of them new; 11 ring-3 ktests (4 new); go-smoke types the whole session above in about 18 s and passed 6/6 repeats (the `user` ktests 8/8); `kernel/proc` and `kernel/tty` pass under `-race -count=3`.

**Surprises:**
- Keeping registers in the nucleus was the right line: fork, execve and signal delivery are each a read-modify-write of one 672-byte image, and the nucleus only checks what Go writes (user RIP/RSP, RFLAGS bits, MXCSR).
- A stale reply was the hardest bug: an interrupt stop's answer must not become RAX, or a thread stopped mid-loop resumes with a syscall result in a register it was using. Only a syscall's answer sets RAX now.
- `kill(-1)` meant "the group of pid 1" to our first code and "every process" to POSIX; `KillGroup` made the group case explicit.
- The console is a Go program, not a process, so it cannot just `tcsetpgrp`. It gives the terminal's foreground group to the shell, forwards signal keys at once and other keys only while a reader waits, and puts leftovers back (`Console.Unread`).
- Keys-file waits that searched the whole log matched an earlier `spin: spinning`; each wait now starts after the previous match.

**Learned:** A request queue plus "give Go the registers" is enough mechanism for all of Unix process semantics; nothing in W10 needed the nucleus to know what a process, file or signal is. Write the fake nucleus with the same resume rules as the real one, or the host tests pass on semantics the kernel does not have.

**Next:** W10-S08 busybox (needs a go-ahead to download its source), then W11: `/dev/fb0` mmap, raw keyboard, Doom.

**Links:** ADR-0004, ADR-0006, PIT-24, BUG-0012

## 2026-11-02 — Wave 9 complete: the first process (M9)

**Did:** W9-S01..S06 and BUG-0012. Static Linux programs run in ring 3 and the Go kernel serves their system calls:

```text
burrow> run /bin/hello
hello from ring 3
burrow> run /bin/crash
about to dereference NULL
[proc] WARN: pid 2 (crash) killed: page fault at 0x0 (rip 0x401018)
run: /bin/crash: killed (status 139)
burrow> run /bin/musl-hello one two
hello from musl in ring 3 (pid 4, 3 args: /bin/musl-hello one two)
malloc works
hostname from the initrd: burrow
```

Nucleus (mechanism): user address spaces (`uspace.c`, a PML4 per process sharing the kernel half), ring-3 threads and a request queue (`uthread.c`), the gate's user path (CR3 classification, kernel stack, SYSRETQ, PIT-24 as a fault), ring-3 exceptions as fault requests, kill that also stops a thread preempted in ring 3. Go (policy): `kernel/proc` with page-level memory over frames it owns, `debug/elf` loading, the Linux entry stack, a dispatcher that handles each request in its own goroutine, 25 Linux syscalls (`POSIX_COVERAGE.md`), faults mapped to the status a shell would show.

**Numbers:** 7 ring-3 ktests on hand-assembled machine code; the `proc` package tested against a fake nucleus under the race detector; GC stress footprint now tracks the runtime's own numbers (47-64 MiB "from the OS") instead of all of RAM.

**Surprises:**
- ADR-0004's caller test (return RIP in Go text) is not sound once processes have their own page tables: a process could map code there. CR3 is: Go never runs on a process's CR3.
- The IRETQ fallback for a non-canonical return RIP (PIT-24) faults in ring 0 just like SYSRET would; turning it into a fault request before returning is simpler and safe.
- BUG-0012 had nothing to do with processes: since W6 the Go kernel's memory was backed eagerly and `MADV_DONTNEED` never freed a frame. Go maps sparse read-write metadata and counts on Linux's demand paging; the GC stress used all 123 MiB while Go counted 47. A software PTE bit for "zero-fill on demand" fixed both, and the same marker serves lazy commit and discard.
- musl-hello needed nothing new: every call it made was already in the table.

**Learned:** Mechanism in C, policy in Go held up: the nucleus never learned what a file or a process is, and every Go piece runs on the host against a fake. And when the kernel hosts a runtime, implement the platform's *contracts* (demand paging, zero-fill), not just its calls.

**Next:** W10: fork (copy-on-write), execve, wait4, pipes and dup, signals with the TTY (`^C`), busybox `sh`.

**Links:** ADR-0004 (amended), ADR-0006, PIT-05, PIT-24, BUG-0012

## 2026-11-02 — Wave 8 complete: files and pixels (M8)

**Did:** W8-S01..S07. The Go kernel has a file tree and a screen:

```text
[initrd] INFO: tarfs: 2 files, 2 directories, 223 bytes
[ata] INFO: hda: "QEMU HARDDISK", 8192 sectors (4096 KiB)
[bga] INFO: 1024x768x32 linear framebuffer at 0xfd000000 (pitch 4096)
[block] INFO: hda sector 1: boot manifest "BRWMNFST" found
[fat] INFO: hdb: FAT32 volume "BURROWDATA" mounted at /mnt
burrow> cat /etc/motd
   Welcome to Burrow.
```

Nucleus: `nu_bootinfo` and a new `nu_mmio_map` (ABI 0x1005). Go: `bootinfo` parses the module table; the VFS is a mount table over `io/fs` with `/` = tarfs over the initrd (zero copy), `/tmp` = memfs, `/dev` = devfs, `/mnt` = FAT32 on the second IDE disk. The block layer has one conformance test that the RAM disk and the Go ATA driver both pass; the ATA driver reads the same manifest sector the loader read. `bga` sets 1024x768x32 and maps BAR0; `gfx` draws a gradient, a hand-drawn 5x8 font and a scrolling text console that mirrors the serial console. `make go-smoke` now also checks pixels of QEMU `screendump`s.

**Numbers:** every file system (memfs, tarfs, the VFS with nested and shadowing mounts, FAT32 with long Unicode names) passes `testing/fstest.TestFS`; FAT32 images are 34 MiB (one 512-byte sector per cluster needs 65525 clusters); the screen check is 8 pixels.

**Surprises:**
- Building the VFS on `io/fs` paid off at once: `fstest.TestFS` found nothing to fix, because it tests what we would have forgotten (ReadDir in every chunk size, Stat names of mount roots).
- `/dev/console` first printed only on serial: it was bound to the serial writer before the screen existed. One console output that the screen joins later fixed it.
- `bga` starts before `pci` (name order), so PCI enumeration is a cached `pci.Scan` that any driver may call first.
- Make cannot depend on a file whose name has a space; heredocs in this environment halve backslashes (three times this wave: always write such files with the editor).
- A Python bytecode cache slipped into a commit when `ppm2png.py` imported `checkscreen`: `__pycache__/` is ignored now.

**Learned:** Standard interfaces are test suites for free (`io/fs` + `fstest`, `io.ReaderAt`). And a screenshot is a fine assertion: pick pixels whose colour the layout guarantees.

**Next:** W9: the first process: address spaces, a user ELF loader, ring 3, the syscall ring, and `/bin/hello` from the initrd.

**Links:** PIT-27, ADR-0005; NUCLEUS_ABI 0x1002, 0x1005

## 2026-11-02 — Wave 7 complete: Go drives hardware (M7)

**Did:** W7-S01..S07. Interrupts reach Go as thread wake-ups (ADR-0005), and the first drivers are plain Go:

```text
[go-selftest] irq ok (10 pit one-shots on line 0 in 96.3ms, 48 sleeps elsewhere meanwhile)
[pci] INFO: 00:01.1 8086:7010 ide controller (class 01.01.80, irq 0)
[ps2] INFO: keyboard ready on irq 1 (scancode set 1 via translation)
[driver] INFO: started: [pci ps2 serial]
burrow> echo hello from the keyboard
hello from the keyboard
```

Nucleus: `irq.c` (8259 in virtual-wire mode, one waiter per line, pending flag, spurious IRQ 7/15) behind `nu_irq_wait`/`nu_irq_ack`, with ktests driven by PIT channel 0 one-shots. Go: `amd64.IRQ` + `driver.Serve`, a driver registry (`init()` registration, name order), PCI enumeration, the 8042 + a set 1 decoder, COM1 receive interrupts, and a console line editor with `help echo lspci uptime mem poweroff`. `make go-smoke` now *types*: QEMU's monitor `sendkey` feeds the PS/2 keyboard, including a backspace. `hostsim` runs the same drivers and console on a fake 8042, 16550 and IRQ lines under `go test -race`. The panic path: a `kargs` boot module becomes argv, REG-0006 checks a Go panic prints every goroutine and fails the run, REG-0007 checks that a nil write in Go is reported as such with the `go tool addr2line` command (PIT-07).

**Numbers:** first IRQ wake-up 10.96 ms for a 10 ms one-shot (the extra ~1 ms is the idle thread waiting for the next tick); 10 one-shots in 96 ms with another goroutine still running; PCI scan of 8192 slots unnoticeable in boot time; 3 goroutines at the console (main, keyboard, serial).

**Surprises:**
- The Go IRQ path worked the first time because `syscall.Syscall` already does the right thing: the runtime treats a thread parked in `nu_irq_wait` like one in `read(2)` and moves its P elsewhere.
- The 8259 latches edges while a line is masked, so the first wait after boot returns at once for the BIOS's 18.2 Hz PIT ticks. Tests either drain it (C) or tolerate one short round (Go).
- QEMU cannot create a Unix socket on WSL's `/mnt/c` (DrvFs): the smoke monitor socket lives in `/tmp`.
- `go vet` rejects a `ReadByte() (byte, bool)` method: names like `ReadByte` promise the `io.ByteReader` signature.
- hostsim's first piped demo interleaved serial and keyboard bytes: two input devices typing at once really do interleave; the simulator now waits for each line's prompt.

**Learned:** Keep the ISR boring (mask, EOI, wake) and the driver ordinary Go: every test above except the QEMU ones runs on the host, and the QEMU ones only confirm the wiring.

**Next:** W8: initrd + VFS, devfs, the BGA framebuffer (a PCI BAR from `pci.Devices()`), and the ATA driver in Go.

**Links:** ADR-0005, PIT-07, PIT-28, PIT-29, BUG-0010, REG-0006, REG-0007

## 2026-11-02 — Wave 6 complete: Go says hello from ring 0 (M6)

**Did:** W6-S01..S12. The stock Go 1.27.3 runtime, built as an ordinary static `GOOS=linux` binary with no changes to the toolchain or the runtime, now runs in ring 0 on the C nucleus:

```text
[kernel] INFO: hello from Go (Burrow kernel)
BURROW:GO:OK goroutine says 42
[go-selftest] sleep ok (10 x 100ms, longest 102.63ms)
[go-selftest] goroutine_ping_pong ok (100000 round trips in 444ms)
[go-selftest] gc_stress ok (1280 MiB allocated, 68 GC cycles, peak heap 55 MiB)
BURROW:GO:SELFTEST:OK
```

The path, one slice at a time: a census on the host (23 syscalls in 1.27, `eventfd2` and `prlimit64` new since 1.22); the kernel ELF as a boot module; a SYSCALL gate that classifies ring-0 callers by RIP and returns with IRETQ; a table-driven shim with "log once" for unknown numbers; `execve`-style start-up (segments, 1 MiB stack, argc/argv/envp/auxv, a trap frame entered with IRETQ); then one boot per missing syscall: `arch_prctl` -> `mmap` -> `clone` -> `futex` -> hello. `eventfd2` + `epoll` made timers work, and the self-tests check the language runtime, goroutines, locks and the GC under 10x the machine's RAM. `make census` catches a Go upgrade that needs a new syscall before it reaches the nucleus.

**Numbers:** 30 syscalls in the shim plus 2 nucleus-private calls; Go kernel 1.5 MiB; `make go-smoke` about 7 s; go.img boot to `BURROW:GO:OK` in about 2 s, most of it reading the ELF over ATA PIO.

**Surprises:**
- Each step was visible: the runtime prints its own stack traces through `write(2)` from the moment `write` works, so every missing piece named itself (`fatal error: runtime: cannot allocate memory` at `mallocinit`, `failed to create new OS thread (errno=38)` at `newm`).
- Go 1.27 places its heap at a random hint (`0x19f9...`), not `0xc000000000`: honouring free hints was enough.
- BUG-0009: Go 1.27 does not clear scavenged spans because Linux's `MADV_DONTNEED` zero-fills. A "harmless" no-op `madvise` turned into a 1-in-8 heap corruption that only the GC stress could show. Finding it took a frame-allocator bitmap (to rule out physical aliasing), memory peeks in the trap report (to see the stale bytes), and reading `mheap.go`.
- After `select {}` with nothing else alive, the runtime correctly reported "all goroutines are asleep - deadlock!" and called `exit_group`: the kernel now idles in `time.Sleep`.

**Learned:** Implement the contract, not the name: `madvise`, `MAP_FIXED` (zeroes) and futex timeouts of 0 ("do not wait") all have semantics that a runtime builds on. Reading the consumer's source is part of implementing a syscall.

**Next:** W7: Go drivers on the nucleus: `nu_irq_wait/ack`, PS/2 keyboard, a console, and a page-fault report the Go side can understand (PIT-07). Consider a lazy-commit #PF handler so the scavenger can really return memory.

**Links:** BUG-0008, BUG-0009, ADR-0003, ADR-0004, PIT-01, PIT-02, PIT-04, PIT-05, PIT-06, PIT-09

## 2026-11-02 — Wave 5 complete: nucleus core (M5)

**Did:** W5-S01..S12. `common/` for code shared by loader and nucleus. Own GDT/TSS (selectors ordered for SYSRET, three IST stacks); IDT with 256 generated stubs, a trap frame that is moved from IST1 onto the current thread's kernel stack, register dumps for unhandled faults and a `.fixup` table for faults that are allowed. `ktest`: the loader passes extra payloads as boot modules, the nucleus runs the named tests, `make ktest` boots one image per suite (`trap`, `mm`, `irq`, `sched`). PMM (pure free list + E820 minus boot modules), VMM (map/unmap/protect/translate via the physmap, identity map dropped, CR0.WP on), reserve/commit regions for the Go runtime, 8259 masked and LAPIC on, TSC + LAPIC timer at 1 kHz, preemptive threads with guard-paged kernel stacks, IRQ-saving spinlocks, futexes, per-thread SSE state.

**Numbers:** 809 host checks; 26 ktests in 4 suites; 5 regression cases. 10,000 futex round trips in 16 ms (30,000 switches). TSC/LAPIC agreement: 98-100 ticks per 100 ms.

**Surprises:**
- CR0.WP was off: W4's read-only nucleus text was writable from ring 0 (BUG-0006). Only a test that actually tried the write found it.
- The plan's "IST1 for interrupts" and "switch threads from the timer" conflict: two threads would share one IST stack. Copying the frame to the thread's own kernel stack in `trap.asm` resolved it without giving up IST (Go goroutine stacks are too small to take a trap frame).
- Wake-before-switch left a running thread marked runnable (BUG-0007), found by reading, not by a test.
- Every test passed the first time it ran except where the test itself was wrong; deliberately breaking the FXSAVE path proved the SSE test can fail.

**Learned:** A `.fixup` table (fault RIP -> resume RIP) makes "this access must fault" a one-line test, and it is the same mechanism the Go kernel will need for copyin/copyout.

**Next:** W6: syscall gate, Linux shim for the Go runtime, load the Go kernel ELF and run `runtime.main` in ring 0. Read GO_RUNTIME_HOSTING.md first.

**Links:** BUG-0006, BUG-0007, PIT-05, PIT-21, PIT-22, PIT-23

## 2026-11-02 — Wave 4 complete: higher-half nucleus (M4)

**Did:** W4-S01..S09. A C nucleus ELF linked at `0xffffffff80100000` with one PT_LOAD per permission set. The loader gained pure, host-tested modules: `palloc` (bump frame allocator over E820), `image` (read a manifest entry through `block_device` + CRC), `elf_load` (validate every segment before copying any), `paging` + `vmsetup` (4-level tables: identity 1 GiB, physmap of all RAM at `0xffff800000000000` with NX, nucleus with 4 KiB W^X pages). boot_info v2 adds `physmap_base` and a module list at `0x1100`. The loader enables EFER.NXE, loads CR3 and jumps with `RDI = physmap + 0x1000`; the nucleus validates v2 through the physmap and prints `BURROW:NUCLEUS:OK`. New smoke file `boot_to_nucleus.txt` (checked with `boot_to_loader.txt` on the same boot), REG-0004 (corrupt nucleus), GitHub Actions CI running `make test` and the Mermaid check.

**Numbers:** 152 host C checks; nucleus 10 KiB on disk; page tables 8 frames for 128 MiB of RAM.

**Surprises:**
- The regression harness silently dropped a case whose mutator failed (BUG-0005). The totals looked fine.
- Two segments sharing a page would break W^X; `pt_map` refusing to overwrite turns that into a loud `already mapped` instead of a silent permission merge.
- `kprintf` has no `-` flag: `%-14s` printed literally and shifted every argument after it. Caught by reading the smoke log, not by a test.

**Learned:** Giving `paging.c` fake physical addresses in host tests (a pool at `0x40000000`) proves it never mixes a physical address with a pointer, which identity-mapped QEMU runs could never show.

**Next:** W5 nucleus: GDT/TSS/IDT first, so a fault prints a register dump instead of a silent triple fault. Then a PMM fed by the E820 map minus the boot modules.

**Links:** BUG-0005, PIT-20, PIT-21

## 2026-11-02 — Waves 0-3 complete: boot to C loader (M0-M3)

**Did:** Built the repo skeleton, contracts (`boot_info.h`, `manifest.h`, `boot_abi.inc`), `mkimage` with Go tests, stage1, stage2 (A20, E820, manifest, chunked load, CPUID, boot_info, protected mode, CRC verify, identity paging, long mode), and the C loader (kprintf on `char_sink`, serial, memory map, manifest validation, self-CRC, ATA PIO cross-check). Harness: 39 host C checks under ASan/UBSan, Go tests, ordered-marker smoke test with debug-exit, regression suite of mutated images.

**Numbers:** stage1 282/446 bytes, stage2 1502/16384, loader 6024 bytes. Full `make test` under 5 seconds.

**Surprises:**
- First QEMU boot of the full chain passed on the first try, because each contract had been written down before the code.
- The regression suite found a *design* flaw (BUG-0003): a corrupted loader cannot be trusted to check itself. Fixed by verifying in stage2 before executing ("verify before you execute").
- Failure paths that just `hlt` made regression tests wait for a 20 s timeout. Adding debug-exit to every failure path took the suite from 40 s to 0.5 s.

**Learned:** Testing negative paths (broken images) is as valuable as the happy path; it tested our *error handling*, which nothing else exercises.

**Next:** W4 higher-half hand-off. Read MEMORY_MAP §2 first.

## 2026-11-02 — Research and design (pre-W0)

**Did:** Surveyed Biscuit, eggos, gopher-os, TamaGo, the GOOS=none proposal, TinyGo, gVisor, doomgeneric ([RESEARCH.md](../01-research/RESEARCH.md)). Chose: asm → C loader → resident C nucleus → stock Go kernel on a Linux-syscall shim (ADR-0001, ADR-0003).

**Experiment:** Ran a syscall census of a Go 1.22 program (22 distinct syscalls). Discovered the runtime reserves >1 GiB of `PROT_NONE` address space at start-up, passes TLS via `CLONE_SETTLS`, and checks fds 0-2 with `fcntl`. Linking at `-T 0x7e0000000000` works. All three went straight into the design (PIT-05, GO_RUNTIME_HOSTING §4).

**Surprise:** Compile-checking the Doom backend snippet found that `<linux/input.h>` and `doomkeys.h` both define `KEY_ENTER` (BUG-0004).

**Next:** W0.
