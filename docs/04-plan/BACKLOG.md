# Backlog: Epics, Features, User Stories

Story format: *As a <role>, I want <capability>, so that <benefit>.* Roles: **learner** (you, reading/typing), **kernel dev** (you, building on lower layers), **user** (someone running Burrow), **porter** (someone bringing software over).

Status: `[x]` done · `[ ]` open. Waves in brackets show where stories are scheduled.

---

## E00 Project Foundations & Living Docs
*Exit: one command builds and tests everything; docs explain every decision.*

- **E00.F01 Build system** [W0]
  - [x] E00.F01.S01 As a learner, I want `make` / `make run` / `make test`, so that I never memorise toolchain flags.
  - [x] E00.F01.S02 As a learner, I want `make sizes`, so that I see fixed-size budgets before they overflow.
  - [x] E00.F01.S03 As a kernel dev, I want a CI workflow (GitHub Actions running `make test`), so that regressions are caught on every push. [W4]
- **E00.F02 Test harness** [W0, every wave]
  - [x] E00.F02.S01 Host C unit tests with sanitizers.
  - [x] E00.F02.S02 QEMU smoke test with ordered markers and debug-exit status.
  - [x] E00.F02.S03 Regression runner that boots mutated images.
  - [x] E00.F02.S04 In-kernel test runner (`ktest`) selected by boot flag, for nucleus and Go code that cannot run on the host. [W5]
  - [x] E00.F02.S05 Go runtime syscall census as a regression test. [W6]
- **E00.F03 Living documentation** [W0, every wave]
  - [x] E00.F03.S01 DEVLOG, BUGS_AND_MITIGATIONS, RISKS, ADRs, architecture with Mermaid.
  - [x] E00.F03.S02 Mermaid diagrams validated in CI. [W4]

## E01 Boot Sector (stage1)
*Exit: BIOS loads us, we load stage2 reliably and fail loudly otherwise.* — all done in W1.
- [x] E01.F01.S01 As a learner, I want to see text from my own 512 bytes, so that I know the machine runs my code.
- [x] E01.F01.S02 As a kernel dev, I want stage2 loaded with LBA reads, so that disk geometry never matters.
- [x] E01.F01.S03 As a kernel dev, I want one-letter failure codes plus a test exit, so that boot failures are diagnosable.

## E02 Stage2 & CPU Modes
*Exit: 64-bit code is running with a verified loader and a complete boot_info.* — done in W2.
- **E02.F01 Real-mode services**: [x] A20 (3 methods) · [x] E820 map · [x] manifest read · [x] chunked loader load.
- **E02.F02 Mode switches**: [x] GDT + protected mode · [x] identity paging 1 GiB · [x] long mode + call C.
- **E02.F03 Safety**: [x] CPUID long-mode check · [x] CRC-verify loader before executing (BUG-0003).
- [ ] E02.F04.S01 As a user, I want a VBE graphics mode set in stage2, so that real hardware without Bochs-VBE still gets a framebuffer. [W21]

## E03 C Loader
*Exit: the loader loads, verifies and starts the nucleus and stages the Go kernel + initrd.*
- **E03.F01 Freestanding C runtime**: [x] entry/BSS · [x] string · [x] kprintf on char_sink · [x] serial.
- **E03.F02 Input verification**: [x] boot_info · [x] memory map · [x] manifest · [x] self-CRC · [x] ATA cross-check.
- **E03.F03 Image loading** [W4]
  - [x] S01 As a kernel dev, I want a physical frame allocator for the loader (bump allocator over E820), so that images land in free RAM.
  - [x] S02 As a kernel dev, I want to read a manifest entry into RAM through `block_device` and verify its CRC.
  - [x] S03 As a kernel dev, I want ELF64 PT_LOAD segments copied to their physical addresses with .bss zeroed.
- **E03.F04 Higher half hand-off** [W4]
  - [x] S01 Page-table builder (4 KiB + 2 MiB pages) as a pure module with host tests.
  - [x] S02 Map identity (temporary), physmap at `0xFFFF800000000000`, nucleus at `0xFFFFFFFF80000000`.
  - [x] S03 boot_info v2: module list (nucleus, kernel, initrd physical ranges), physmap base.
  - [x] S04 Jump to nucleus; nucleus prints `BURROW:NUCLEUS:OK`.

## E04 Nucleus Core (C + asm mechanisms)
*Exit: robust ring-0 base with exceptions, memory, threads, time, futexes.* [W5]
- **E04.F01 CPU tables**: GDT with TSS (IST1 interrupts, IST2 double fault, IST3 NMI); IDT with 256 stubs; exception handler with full register dump and decoded page-fault error code.
- **E04.F02 Physical memory**: free-list/bitmap PMM from E820 minus everything used (unit-tested on host with synthetic maps).
- **E04.F03 Virtual memory**: map/unmap/protect/translate API over 4-level tables via physmap; reserve/commit regions (needed by Go, PIT-05).
- **E04.F04 Time**: PIT for calibration, LAPIC timer for ticks, TSC-based monotonic ns.
- **E04.F05 Threads**: thread struct, kernel stacks, context switch in asm, round-robin preemptive scheduler, sleep, idle `hlt`.
- **E04.F06 Sync**: spinlocks (IRQ-safe), futex wait/wake with timeouts.
- **E04.F07 Self-tests**: `ktest` suite: PMM exhaustion, page-fault on guard page, two threads ping-pong 10k times.

## E05 Go Runtime Hosting
*Exit: unmodified Go binary runs in ring 0 with goroutines, channels, timers, GC.* [W6]
- **E05.F01 Spike** [spike]: census with pinned Go; high-address link; mmap-hint behaviour. Output: ADR-0003/0004 confirmed or plan B.
- **E05.F02 Loading**: loader stages `kernel.elf`; nucleus maps it (supervisor), records text range, builds Linux initial stack, enables SSE.
- **E05.F03 Syscall gate**: LSTAR/STAR/FMASK setup; gate with stack switch; IRETQ return; shim table with census-on-failure.
- **E05.F04 Shim, in discovery order**: write → arch_prctl → mmap/munmap/madvise (reserve/commit) → signals stubs → fcntl/openat/getrlimit/affinity → clone → futex → nanosleep/yield/clock_gettime → pipe2/epoll minimal.
- **E05.F05 Proof**: `BURROW:GO:OK`; goroutine ping-pong; GC stress (allocate 10x RAM over time); timers (`time.Sleep` accuracy within 10%).

## E06 Go Kernel Framework & Drivers
*Exit: Go kernel structure that new drivers plug into without edits elsewhere.* [W7]
- E06.F01 HAL + `arch/amd64` (ports, MMIO via physmap, nucleus call wrappers).
- E06.F02 Driver registry: `Driver` interface (`Name`, `Probe`, `Start`), registered in `init()` (Open/Closed).
- E06.F03 IRQ lines as goroutines (ADR-0005) + PS/2 keyboard driver.
- E06.F04 Console: line editor on serial + keyboard; kernel log levels.
- E06.F05 Panic path: Go panic output + goroutine dump to serial, then debug-exit failure.
- E06.F06 `hostsim` build tag: run kernel subsystems as a normal Linux process with fake HAL for fast tests.
- E06.F07 PCI enumeration (config space via ports 0xCF8/0xCFC).

## E07 Storage, VFS & Devices
*Exit: a file tree from initrd + device files, framebuffer drawing.* [W8]
- E07.F01 initrd (ustar) staged by loader; `tarfs` read-only FS.
- E07.F02 VFS interfaces (`FS`, `Node`, `File`), mount table, path walk, ramfs (writable `/tmp`).
- E07.F03 devfs: `/dev/console`, `/dev/null`, `/dev/zero`, `/dev/fb0`, `/dev/input/event0`.
- E07.F04 Framebuffer: Bochs/QEMU BGA mode set (ports 0x1CE/0x1CF), linear FB via PCI BAR; text console on it. [W8]
  - [ ] E07.F04.S01 As a kernel dev, I want the firmware's framebuffer (address, geometry, pitch, pixel format) in boot_info, so that nothing above stage2 needs to know how the mode was set. [W21]
  - [ ] E07.F04.S02 As a user, I want the console and `/dev/fb0` to work at whatever geometry and pixel format the firmware gave, so that a screen that is not 1024x768 XRGB still shows Burrow. [W21]
  - [ ] E07.F04.S03 As a user, I want the framebuffer mapped write-combining, so that drawing on real hardware is not one bus transaction per pixel. [W21]
  - [ ] E07.F04.S04 As a porter, I want a smoke test at an odd resolution with a padded pitch, so that a program assuming `pitch == width*4` or 1024x768 fails in the emulator. [W21]
- E07.F05 Block layer + Go ATA PIO driver; conformance tests shared with a RAM disk (Liskov).
- E07.F06 FAT32 read-only (then ext2 later) for a data partition.

## E08 Processes & Syscall Layer
*Exit: static musl programs run in ring 3 with a growing Linux syscall subset.* [W9]
- E08.F01 Address spaces: Go policy (VMAs) + nucleus mechanism (`nu_as_*`), physmap copyin/copyout with fault handling.
- E08.F02 User ELF loader (static, then static-PIE); initial stack (reuse `go_initial_stack.c` logic in Go).
- E08.F03 Ring 3: user GDT entries, TSS RSP0, SYSRET/IRET paths, user page faults → kill process with message.
- E08.F04 Syscall ring + dispatcher goroutines (diagram 7.3).
- E08.F05 First syscalls: `write, read, open, close, lseek, fstat, brk, mmap(anon), munmap, exit, exit_group, getpid, clock_gettime, nanosleep, ioctl(TCGETS->ENOTTY), writev, arch_prctl, set_tid_address, rt_sigprocmask(stub)` (musl start-up needs most of these).
- E08.F06 `POSIX_COVERAGE.md`: living table of every syscall, status and deviations.

## E09 POSIX Userland
*Exit: an interactive busybox shell with pipes.* [W10]
- E09.F01 fork (copy-on-write), execve, wait4, exit status.
- E09.F02 File descriptors: dup/dup2/pipe/fcntl, cwd, openat family.
- E09.F03 Signals: kill, sigaction, default actions, SIGINT from TTY, SIGCHLD.
- E09.F04 TTY line discipline (canonical mode, echo, ^C), `termios` subset.
- E09.F05 busybox static build; `/bin/sh` as PID 1's child; init in Go.

## E10 DOOM
*Exit: Freedoom playable in QEMU at a usable frame rate.* [W11]
- E10.F01 fbdev subset: `FBIOGET_VSCREENINFO`, `FBIOGET_FSCREENINFO`, `mmap` of `/dev/fb0` (MAP_SHARED of device memory).
- E10.F02 evdev subset: `struct input_event` from the PS/2 driver, `O_NONBLOCK` reads.
- E10.F03 Build: doomgeneric + `doomgeneric_burrow.c` + musl, verified on Linux first (fbdev console VM).
- E10.F04 Assets: Freedoom WAD in initrd (or FAT data disk).
- E10.F05 Performance: measure FPS, fix hot spots (likely syscall ring latency, fb copy).
- E10.F06 Stretch: sound via Sound Blaster 16 or AC'97 (`FEATURE_SOUND`).

## E11 Toolchains & Ports
*Exit: we can bring software over quickly.* [W12]
- Lua 5.4 REPL (pure C, easiest first port) · C++ hello with exceptions + `std::thread` (static libstdc++/libc++) · Go user programs (`GOOS=linux`, needs clone/futex in user mode — reuse nucleus) · TinyCC or a C compiler running *on* Burrow · porting guide `docs/06-guides/PORTING.md`.

## E13 A Workbench
*Exit: code can be written, saved and built inside Burrow.* [W13]
- An editor (busybox `vi`; kilo optional) · the screen console as a VT100-subset terminal · ATA writes · writable FAT32 checked by `fsck.fat` · a persistent `/home` and `make run-dev`.

## E14 Desktop Groundwork
*Exit: graphical programs find the kernel interfaces they expect below a toolkit.* [W14]
- [x] E14.F01 As a user, I want more than 128 MiB of RAM, so that toolkits and their fonts fit.
- [x] E14.F02 As a user, I want a mouse (`/dev/input/event1`, `/dev/input/mice`), so that I can point and click.
- [x] E14.F03 As a porter, I want `select`, `getrandom`, `/dev/urandom`, `statx` and the other start-up calls, so that toolkits start without patches.
- [x] E14.F04 As a user, I want a real-time clock, so that files and programs see the real date.
- [x] E14.F05 As a porter, I want `mmap` of regular files, so that fonts map and a dynamic loader becomes possible.
- [x] E14.F06 As a porter, I want `AF_UNIX` sockets (pairs, names, abstract names, `SCM_RIGHTS`), so that an X server and its clients can talk.
- [ ] E14.F08 As a porter, I want named datagram Unix sockets (`bind`, `connect` and `sendto` with an address on `SOCK_DGRAM`), so that `syslog` and other datagram clients reach their servers (W14-S07 left them `-EOPNOTSUPP`).
- [x] E14.F07 As a porter, I want the `KD*`/`VT_*` terminal calls, so that graphics programs can take and give back the screen.

## E15 Qt
*Exit: Qt widget programs run on `/dev/fb0` with keyboard and mouse.* [W15]
- [x] Static qtbase (Core, Gui, Widgets) for musl with `linuxfb` · [x] a syscall census of Qt on Linux · [x] QtCore, then pixels, then input on Burrow · [x] performance budgets · [x] `qtpad`, an editor for `/home`, and `make run-qt`.

## E16 X11 and GTK
*Exit: an X server with a window manager, then GTK programs.* [W16, then later]
- [x] E16.F01 As a user, I want an X server on the framebuffer (TinyX `Xfbdev`) with the console's keyboard and mouse, so that many programs can share the screen. [W16]
- [x] E16.F02 As a porter, I want the X libraries (Xlib, xcb, Xft, fontconfig, core fonts) static for musl, so that X programs build like the other ports. [W16]
- [x] E16.F03 As a user, I want pseudo-terminals (`/dev/ptmx`, `/dev/pts`), so that terminal emulators can run a shell. [W16]
- [x] E16.F04 As a user, I want dwm, st and dmenu started by `startx`, so that I have a tiling desktop. [W16]
- [x] E16.F05 As a user, I want Doom in an X window, so that games and the desktop mix. [W16]
- Later: the 1.20-branch kdrive `Xfbdev` (XKB, XInput 2: GTK3 needs XInput 2) · GTK3 · Qt's `xcb` platform · MIT-SHM (needs E18).

## E17 Networking
*Exit: user programs on Burrow talk TCP/IP to the host and the internet.* [W17]
- [x] E17.F01 As a user, I want a network card (e1000) driven from Go, so that Burrow can send and receive frames. [W17]
- [x] E17.F02 As a user, I want TCP/IP in the kernel (gVisor's `tcpip`, pinned), so that Burrow speaks IPv4 and IPv6. [W17]
- [x] E17.F03 As a porter, I want `AF_INET`/`AF_INET6` sockets with Linux semantics, so that wget, Go's `net/http` and servers work unchanged. [W17]
- [x] E17.F04 As a user, I want names (`/etc/resolv.conf`, `/etc/hosts`) and DNS, so that programs reach hosts by name. [W17]
- Later: DHCP · virtio-net · a browser (NetSurf, framebuffer or X) · ssh (dropbear) · TLS for C programs (mbedTLS).

## E18 Foundations for bigger programs
*Exit: big programs start fast, share memory, and share code.* [W18]

- [x] E18.F01 As a user, I want fork to share memory until a write (copy-on-write), so that forking a big program is cheap. [W18]
- [x] E18.F02 As a user, I want `execve` to read a program's pages as they are touched, so that big programs start fast. [W18]
- [x] E18.F03 As a porter, I want shared memory (`MAP_SHARED`, `/dev/shm`, `memfd_create`, SysV `shm*`), so that programs and the X server share buffers. [W18]
- [x] E18.F04 As a porter, I want dynamic linking (`PT_INTERP`, musl's loader, `dlopen`), so that programs share code and load plugins. [W18]
- Later: shared Qt and X libraries · sound (SB16 or AC'97, OSS `/dev/dsp`) · an absolute pointer (USB tablet or virtio-input: no mouse grab, PIT-32).

## E19 A multi-user Unix
*Exit: people log in, files have owners and permissions, links work, and a Unix file system keeps it all.* [W19]

- [x] E19.F01 As a user, I want ext2 disks read and written (checked by `e2fsck`), so that files keep owners, modes and links across reboots. [W19]
- [x] E19.F02 As a porter, I want user and group ids with the `set*id` calls and set-user-id programs, so that `su`, `login` and servers that drop privileges work. [W19]
- [x] E19.F03 As a user, I want permissions checked on every path, `open` and `execve`, so that one user cannot change another's files. [W19]
- [x] E19.F04 As a porter, I want symbolic and hard links with Linux's path rules (`ELOOP`, `O_NOFOLLOW`, `..` after a link), so that install scripts and build tools work unchanged. [W19]
- [x] E19.F05 As a user, I want to boot from an ext2 root disk to a `login:` prompt and manage users with `passwd` and `adduser`, so that Burrow is a multi-user system. [W19]
- Later: ext3/ext4 (journal, extents) · ACLs and extended attributes · capabilities · `/proc` · ssh (dropbear, with E17).

## E12 Hardening & Evolution
*Exit: a platform worth extending.* [W20, then later]
- [ ] E12.F01 As a kernel dev, I want a hang to leave evidence (every process and thread with what it waits on) and BUG-0023 found, so that races are fixed before more CPUs make them worse. [W20]
- [ ] E12.F02 As a user, I want SMEP, SMAP and NX on, so that a kernel bug cannot run or quietly read user memory. [W20]
- [ ] E12.F03 As a porter, I want XSAVE state and a CPU with AVX, so that programs using AVX run and keep their registers. [W20]
- [ ] E12.F04 As a user, I want ACPI: the machine's CPUs from the MADT and a real poweroff, so that Burrow knows what it runs on and turns it off. [W20]
- [ ] E12.F05 As a user, I want every CPU running threads (APs started, one run queue, IPIs, TLB shootdown), so that programs and the Go kernel run in parallel. [W20]
- [ ] E12.F06 As a porter, I want `nproc` and the Go runtime to see every CPU (`GOMAXPROCS>1`), so that parallel programs speed up. [W20]
- Later: per-CPU run queues and balancing · an I/O APIC · a tickless timer · UEFI front-end producing boot_info · virtio-blk/virtio-net · real-hardware boot.
