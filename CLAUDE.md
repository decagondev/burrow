# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Burrow is an educational x86-64 OS: **assembly boot → C loader → C nucleus → Go kernel**, aiming at a POSIX-like system that runs DOOM. Waves 0-4 (boot to a higher-half C nucleus) are done; Wave 5 (nucleus: GDT/IDT, PMM, VMM, threads) is next. Work is planned in "slices" in `docs/04-plan/waves/WAVE-NN-*.md`. Docs start at `docs/README.md`.

## Commands

The build targets Linux/WSL (bash, gcc, nasm, qemu, go 1.22+, python3). On this Windows host, run `make` from WSL, not PowerShell. On non-Linux hosts use a cross compiler: `make CC_TARGET=x86_64-elf-gcc LD_TARGET=x86_64-elf-ld OBJCOPY=x86_64-elf-objcopy`.

```sh
make              # build/disk.img (stage1 + stage2 + loader via tools/mkimage)
make run          # boot in QEMU, serial on stdio (Ctrl-A X quits)
make debug        # QEMU paused on :1234; then: gdb -x scripts/gdbinit
make test         # test-unit + smoke + regress (full suite, ~5 s)
make test-c       # host C unit tests (gcc, ASan+UBSan) -> build/unit_tests
make test-go      # go vet + go test in tools/ and kernel/ (separate modules)
make smoke        # boot headless, assert ordered serial markers
make regress      # boot deliberately broken images, assert they fail loudly
make sizes        # how full stage1 / stage2 / loader are
make docs-check   # parse every Mermaid block (needs node + npm)
make -C snippets check   # reference snippets still compile
```

Single tests:
- Go: `cd kernel && go test ./drivers/uart -run TestName` (or `cd tools && go test ./mkimage -run TestName`).
- C: there is no filter; minitest runs every test listed in `tests/unit/c/test_main.c`. Build and run `build/unit_tests` and read the `ok`/`FAIL` line for your test.

Expected `make test` tail: `N checks, 0 failures`, `SMOKE PASS [boot_to_loader]`, `SMOKE PASS [boot_to_nucleus]`, `M passed, 0 failed`. CI (`.github/workflows/ci.yml`) runs `make test` and `make docs-check`.

## Architecture (big picture)

- **Layers and ownership** (`docs/02-architecture/ARCHITECTURE.md`): stage1 (512 B MBR, only loads stage2) → stage2 (A20, E820, manifest, CPUID, protected mode, CRC-verifies the loader, identity paging, long mode, calls loader with `RDI = boot_info`) → loader (freestanding C: reads the `nucleus` ELF via ATA + CRC, copies its segments to phys 1 MiB, builds identity + physmap + W^X nucleus page tables, upgrades boot_info to v2, loads CR3 and jumps with `RDI = physmap + 0x1000`; does not stay resident) → nucleus (`nucleus/`, linked at `0xFFFFFFFF80100000` with `-mcmodel=kernel`, reuses the loader's serial/kprintf/string sources; *mechanism*: IDT/GDT, PMM/VMM, threads, futexes, syscall gate from W5) → Go kernel (*policy*: drivers, VFS, processes, POSIX).
- **Key trick (ADR-0003):** the Go kernel is a stock `linux/amd64` binary; the nucleus pretends to be a tiny Linux and answers the Go runtime's syscalls. Userland later uses the same Linux ABI via musl. Go never runs interrupt handlers; IRQs become wake-ups (ADR-0005). No allocation in Go on interrupts-off paths.
- **Cross-language contracts** live in `include/burrow/` (`boot_info.h`, `manifest.h`) and are mirrored by hand in `boot/boot_abi.inc` and in `tools/mkimage/manifest.go`. Changing a struct or fixed address means updating all mirrors plus `docs/02-architecture/MEMORY_MAP.md` / `BOOT_PROTOCOL.md`. C side uses `__attribute__((packed))` + `_Static_assert` on offsets; Go has layout tests (`TestManifestLayoutMatchesC`).
- **Disk layout:** LBA 0 stage1, LBA 1 manifest, LBA 2..33 stage2, then payloads named in the manifest (`loader`, `nucleus` = stripped `build/nucleus.disk.elf`; built by `tools/mkimage`).
- **Loader-chosen addresses** live in `loader/layout.h` (nucleus physical window 1-4 MiB, palloc range up to stage2's 1 GiB identity map, `PHYSMAP_BASE`). Addresses inside boot_info stay physical. Stage2 writes boot_info v1 (`BOOTINFO_VERSION_STAGE2`); the loader writes v2 (`BOOTINFO_VERSION`). CRC32 is computed identically in mkimage (Go), stage2 (asm) and loader (C).
- **Testability via interfaces:** C uses small function-pointer "ops" structs (`loader/char_sink.h`, `loader/block_device.h`, `pt_ctx` in `loader/paging.h`); pure modules (kprintf, crc32, manifest, memmap, elf64, elf_load, palloc, paging, vmsetup, image, lib_string) include neither `io.h` nor `cpu.h` and are host-tested. Inline asm lives only in those two headers. Go: interfaces in `kernel/hal`, fakes in `kernel/haltest`; real hardware only in `kernel/arch/amd64` behind build tag `burrow`, so everything else builds and tests on the host.
- `snippets/` holds compile-checked reference code for future waves (nucleus syscall gate, Go IRQ goroutine, Doom backend); it is not part of the build.

## Test conventions (`docs/05-testing/TEST_STRATEGY.md`)

- **New pure C module:** add `tests/unit/c/test_<mod>.c`, add the module `.c` to `UNIT_SRC` in the Makefile, and declare + `RUN()` every test in `test_main.c` (no auto-registration).
- **Serial markers:** format `[component] message`, success `BURROW:<NAME>:OK`. Expect files (`tests/smoke/expect/*.txt`) are **append-only**; never weaken one to make a test pass. Smoke fails on any `PANIC`/`FAIL` line or out-of-order marker. `make smoke` checks every file in `SMOKE_EXPECT` against the same boot.
- The loader's `kprintf` supports only `%% %c %s %d %i %u %x %X %p`, the `0` flag, width and `l`/`ll`/`z`; there is no `-` flag.
- **QEMU exit codes:** guest writes `0x10` to port `0xF4` → exit 33 (pass); `0x11` → 35 (fail); 124 = timeout = hang bug. Every failure path must write debug-exit `0x11` so failures are instant.
- **Regression cases:** add a mutator in `tests/regression/run_all.sh` with `case_run REG-NNNN-name "<expected text>" 35 mutator`; use `lba_of <img> <name>` rather than hard-coding payload LBAs. Every bug gets a test named after its BUG/REG id.
- Smoke logs go to `build/smoke-<name>.log`.

## Coding rules (`docs/06-guides/CODING_STANDARDS.md`)

- Every file starts with a comment: what it does, what it must never do, which doc/ADR explains it. Files < 300 lines, functions < 50.
- Names carry units (`size_bytes`, `count_sectors`, `phys`/`virt` prefixes). No magic numbers outside one constants place per component (`boot_abi.inc`, headers, Go `const` blocks).
- Loader C: C11 freestanding, `-Werror`, `-mgeneral-regs-only` (SSE not enabled), **no mutable initialised globals** (the loader is CRC-verified; see BUG-0003). Hardware access only through `io.h`-style headers.
- Assembly: NASM, document clobbered registers in each routine header, `.name` local labels, constants from `boot_abi.inc`. stage1 has a hard 512-byte budget — check `make sizes`.
- Go: gofmt + go vet clean; errors are values; `panic` only for kernel invariants.

## Workflow (`docs/06-guides/WORKFLOW.md`)

- One slice per commit, message prefixed with the slice id: `W4-S03: load manifest entry with CRC check`. Bug fixes: `BUG-0007: fix ...` with the regression test in the same commit.
- Write the test/marker first and see it fail, then implement.
- Keep the living docs current: `docs/living/DEVLOG.md` (newest first, template at top), `BUGS_AND_MITIGATIONS.md` (every surprise gets a BUG-NNNN; PIT-NN lists anticipated pitfalls per wave), `RISKS.md`. Mark slices `[~]`/`[x]` in the wave file. New design decisions get an ADR from `docs/03-decisions/ADR-0000-template.md`.
