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
