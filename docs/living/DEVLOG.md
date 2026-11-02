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
