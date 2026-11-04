# Burrow

An educational x86-64 operating system: **assembly boot → C loader → C nucleus → Go kernel**, growing toward a POSIX-like system that runs DOOM.

> "Burrow" is a placeholder name (gophers live in burrows). Rename with one search-and-replace.

## Status

| Milestone | State |
|---|---|
| M0-M3: own boot sector → stage2 → long mode → C loader, all verified | **done, tested in QEMU** |
| M4: higher-half nucleus (page tables, physmap, boot_info v2) | **done, tested in QEMU** |
| M5: nucleus (GDT/IDT, traps, PMM, VMM, LAPIC timer, threads, futexes, SSE) | **done, 26 in-kernel tests** |
| M6: stock Go runtime (1.27.3, `GOOS=linux`) running in ring 0 | **done: goroutines, timers, GC** |
| M7: Go drivers (IRQs as goroutine wake-ups, PS/2 keyboard, serial, console, PCI) | **done: type at `burrow>`** |
| M8: files and pixels (initrd, VFS, devfs, FAT32, framebuffer, ATA in Go) | **done: `cat /etc/motd`, a text console on screen** |
| M9: first process (ring 3, static ELF, musl, syscalls served by Go) | **done: `run /bin/musl-hello`** |
| M10: a Unix (fork, exec, pipes, signals, a TTY, a shell) | **done: `sh`, then `ls / \| wc -l` and `^C`; busybox ash** |
| M11: DOOM (doomgeneric + Freedoom on /dev/fb0 and /dev/input/event0) | **done: `make doom doom-smoke`, 100 fps in `make doom-bench`** |
| M12: ports (threads, a Go program, Lua, C++, a C compiler on Burrow) | **done: `gohello`, Lua, C++, `tcc` on Burrow** ([PORTING.md](docs/06-guides/PORTING.md)) |
| M13: a workbench (vi on screen, writable FAT32, a persistent /home) | **done: `make run-dev`** ([WORKBENCH.md](docs/06-guides/WORKBENCH.md)) |
| M14: desktop groundwork (a mouse, Unix sockets, a clock, file mmap, console modes) | **done: `make desk-smoke`** |
| M15: Qt widget programs on /dev/fb0 | **done: `make run-qt`, `qtpad`** |
| M16: an X11 desktop (TinyX, dwm, st, dmenu, Doom in a window) | **done: `make run-desktop`, then `startx`** ([DESKTOP.md](docs/06-guides/DESKTOP.md)) |
| M17: networking (e1000, gVisor's TCP/IP, `AF_INET` sockets, wget, servers inside Burrow) | **in progress: `make net-smoke`** |

## Quick start

```sh
# Ubuntu/WSL: sudo apt install build-essential nasm qemu-system-x86 gdb golang python3 musl-tools
make            # build/disk.img
make run        # boot it; serial output in your terminal (Ctrl-A X to quit)
make test       # host unit tests + QEMU smoke + in-kernel ktests + regression suite
make ktest      # just the in-kernel test suites (tests/ktest/*.txt)
make run-go     # boot nucleus + Go kernel (build/go.img); type at burrow> (help, lspci, mem)
make hostsim    # the same console and drivers on simulated hardware, no QEMU
make run-go GO_DISPLAY=gtk   # the same, with the screen in a window (WSLg works)
make debug      # QEMU waits for gdb -x scripts/gdbinit
make sizes      # how full stage1 / stage2 / loader are
```

Expected `make test` tail:

```text
855 checks, 0 failures
CENSUS PASS (go1.27.3, 24 syscalls, all in the shim)
FAT INTEROP PASS (2 files read from a mkfs.fat image; kernel writes pass fsck.fat on 2 images)   # SKIP without dosfstools + mtools
SMOKE PASS [boot_to_loader] (54 log lines)
SMOKE PASS [boot_to_nucleus] (54 log lines)
SCREEN PASS (8 checks)
SMOKE PASS [boot_to_go] (196 log lines)
SCREEN PASS (14 checks)
SMOKE PASS [devices] (115 log lines)
SMOKE PASS [disk] (106 log lines)
DISK WRITE PASS (64 sectors at the end of build/scratch.img)
SMOKE PASS [ports] (113 log lines)
SMOKE PASS [ports-dl] (124 log lines)  # SKIP until make ports has built them
SCREEN PASS (8 checks)                 # dev: SKIP until make busybox ports
SMOKE PASS [dev-1] (817 log lines)
SMOKE PASS [dev-2] (109 log lines)
HOME DISK PASS (fsck.fat after two boots)
SCREEN PASS (13 checks)                # doom: SKIP until make doom has built it
SMOKE PASS [doom] (138 log lines)
SMOKE PASS [busybox] (119 log lines)   # SKIP until make busybox has built it
SMOKE PASS [irq] (68 log lines)
SMOKE PASS [mm] (66 log lines)
SMOKE PASS [sched] (79 log lines)
SMOKE PASS [syscall] (68 log lines)
SMOKE PASS [trap] (63 log lines)
SMOKE PASS [user] (71 log lines)
7 passed, 0 failed
```

## Repository map

```text
boot/          stage1.asm (MBR), stage2.asm (real→protected→long), shared .inc files
include/burrow cross-language contracts: boot_info.h, manifest.h
common/        freestanding C shared by loader and nucleus: kprintf, string, CRC, serial
loader/        freestanding C loader: serial, kprintf, CRC, manifest, ELF64, ATA PIO,
               frame allocator, page tables, nucleus hand-off
nucleus/       higher-half C nucleus: GDT/IDT/traps, PMM, VMM, LAPIC, time, threads,
               futexes, SSE, and the ktest in-kernel test runner
kernel/        Go module: the Go kernel (cmd/kernel), driver registry, drivers (uart, ps2,
               pci, pit, bga, ata), vfs + fs/{memfs,tarfs,devfs,fat}, block, gfx,
               console, hostsim, selftest, hal + haltest fakes, klog, mkfat (cmd),
               arch/amd64 (tag burrow: port I/O, IRQ lines, nucleus calls)
tools/mkimage  Go host tool that builds disk.img (+ tests)
tools/census   the Go runtime syscall census workload (tests/regression/census.sh)
initrd/        the initial file tree, packed into build/initrd.tar (mounted at /)
datadisk/      files for build/data.img, the FAT32 second disk (mounted at /mnt)
user/          user programs (asm, C with musl: sh and tools), installed in the initrd's /bin
tools/doccheck Mermaid diagram validator (Node)
tests/         unit/c (host C tests), smoke/ (QEMU markers), ktest/ (in-kernel suites),
               regression/ (broken images)
snippets/      compile-checked reference code for future waves (nucleus, Go IRQ, Doom backend)
scripts/       gdbinit, sizes.sh
docs/          vision, research, architecture, ADRs, plan, testing, guides, living docs
```

## Documentation

Start at [docs/README.md](docs/README.md). The living documents you update every session are [DEVLOG](docs/living/DEVLOG.md), [BUGS_AND_MITIGATIONS](docs/living/BUGS_AND_MITIGATIONS.md) and [RISKS](docs/living/RISKS.md).

## Licence

Not chosen yet. Pick one (MIT/BSD-2 are common for hobby OSes) before publishing. Third-party code you port keeps its own licence; Doom engine code is GPL-2.0, Freedoom assets are BSD-3-Clause.
