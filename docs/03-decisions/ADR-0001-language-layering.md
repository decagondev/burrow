# ADR-0001: Assembly, then C, then Go

- **Status:** Accepted
- **Date:** 2026-11-02
- **Wave:** W0

## Context
We want an educational OS whose kernel is written in Go, but Go cannot run before a CPU is in long mode with paging, a stack, TLS and an allocator. Something must get us there, and something must keep handling the parts of the machine where Go's runtime (GC, stack growth, preemption points) is unsafe.

## Options
1. **All Go + Go assembly** (Biscuit, gopher-os). Fewest languages; but rt0 work in Go asm is obscure, and you need a modified runtime.
2. **Asm boot + Go kernel via GRUB/Multiboot** (eggos). Skips the bootloader learning; hides the most educational part.
3. **Asm -> C loader -> C nucleus -> Go kernel.** Each language used where it shines; C is the readable bridge.

## Decision
Option 3. Assembly for the MBR and mode switches; freestanding C for the loader and the resident nucleus (mechanisms); Go for everything with policy.

## Consequences
- Three toolchains (nasm, gcc, go). Mitigated by one Makefile.
- Cross-language contracts (boot_info, manifest, nucleus ABI) need mirrored definitions and tests on both sides.
- Learners see every step; the C layer is small enough to read in an evening.
