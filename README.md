# Burrow

An educational x86-64 operating system: **assembly boot → C loader → C nucleus → Go kernel**, growing toward a POSIX-like system that runs DOOM.

> "Burrow" is a placeholder name (gophers live in burrows). Rename with one search-and-replace.

## Status

| Milestone | State |
|---|---|
| M0-M3: own boot sector → stage2 → long mode → C loader, all verified | **done, tested in QEMU** |
| M4: higher-half nucleus (page tables, physmap, boot_info v2) | **done, tested in QEMU** |
| M5: nucleus (GDT/IDT, traps, PMM, VMM, LAPIC timer, threads, futexes, SSE) | **done, 26 in-kernel tests** |
| M6: stock Go runtime running in ring 0 | next ([Wave 6](docs/04-plan/waves/WAVE-06-go-runtime.md)) |
| M11: DOOM | planned (W11) |

## Quick start

```sh
# Ubuntu/WSL: sudo apt install build-essential nasm qemu-system-x86 gdb golang python3
make            # build/disk.img
make run        # boot it; serial output in your terminal (Ctrl-A X to quit)
make test       # host unit tests + QEMU smoke + in-kernel ktests + regression suite
make ktest      # just the in-kernel test suites (tests/ktest/*.txt)
make debug      # QEMU waits for gdb -x scripts/gdbinit
make sizes      # how full stage1 / stage2 / loader are
```

Expected `make test` tail:

```text
809 checks, 0 failures
SMOKE PASS [boot_to_loader] (52 log lines)
SMOKE PASS [boot_to_nucleus] (52 log lines)
SMOKE PASS [irq] (61 log lines)
SMOKE PASS [mm] (64 log lines)
SMOKE PASS [sched] (77 log lines)
SMOKE PASS [trap] (61 log lines)
5 passed, 0 failed
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
kernel/        Go module: hal interfaces, uart driver, klog, arch/amd64 (tag burrow)
tools/mkimage  Go host tool that builds disk.img (+ tests)
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
