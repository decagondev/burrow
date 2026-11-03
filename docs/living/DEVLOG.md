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
