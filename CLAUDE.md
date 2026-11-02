# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Burrow is an educational x86-64 OS: **assembly boot → C loader → C nucleus → Go kernel**, aiming at a POSIX-like system that runs DOOM. Waves 0-7 are done: boot chain, nucleus core (traps, memory, timer, threads, futexes, SSE), the stock Go 1.27.3 runtime running in ring 0 on the nucleus's Linux-syscall shim, and Go drivers (IRQs as wake-ups, PS/2, serial, PCI) with a console. Waves 8-12 (`docs/04-plan/waves/WAVES-07-12.md`: VFS and framebuffer, user space, Doom) are next. Work is planned in "slices" in `docs/04-plan/waves/WAVE-NN-*.md`. Docs start at `docs/README.md`.

## Commands

The build targets Linux/WSL (bash, gcc, nasm, qemu, go 1.22+, python3). On this Windows host, run `make` from WSL, not PowerShell. On non-Linux hosts use a cross compiler: `make CC_TARGET=x86_64-elf-gcc LD_TARGET=x86_64-elf-ld OBJCOPY=x86_64-elf-objcopy`.

```sh
make              # build/disk.img (stage1 + stage2 + loader via tools/mkimage)
make run          # boot in QEMU, serial on stdio (Ctrl-A X quits)
make debug        # QEMU paused on :1234; then: gdb -x scripts/gdbinit
make test         # test-unit + census + smoke + go-smoke + ktest + regress (full suite)
make test-c       # host C unit tests (gcc, ASan+UBSan) -> build/unit_tests
make test-go      # go vet + go test in tools/ and kernel/ (separate modules)
make smoke        # boot headless, assert ordered serial markers
make ktest        # boot one image per tests/ktest/<suite>.txt, run the in-kernel tests
make go-smoke     # boot build/go.img (nucleus + Go kernel), check tests/smoke/expect/boot_to_go.txt
make run-go       # the same image, interactive
make census       # Go runtime syscalls on the host (strace) vs tests/regression/census-go1.27.txt
make hostsim      # the Go console + drivers on simulated hardware (kernel/hostsim), no QEMU
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

- **Layers and ownership** (`docs/02-architecture/ARCHITECTURE.md`): stage1 (512 B MBR, only loads stage2) → stage2 (A20, E820, manifest, CPUID, protected mode, CRC-verifies the loader, identity paging, long mode, calls loader with `RDI = boot_info`) → loader (freestanding C: reads the `nucleus` ELF via ATA + CRC, copies its segments to phys 1 MiB, builds identity + physmap + W^X nucleus page tables, upgrades boot_info to v2, loads CR3 and jumps with `RDI = physmap + 0x1000`; does not stay resident) → nucleus (`nucleus/`, linked at `0xFFFFFFFF80100000` with `-mcmodel=kernel`, shares `common/` with the loader; *mechanism*: IDT/GDT, PMM/VMM, threads, futexes, syscall gate from W5) → Go kernel (*policy*: drivers, VFS, processes, POSIX).
- **Key trick (ADR-0003):** the Go kernel is a stock `linux/amd64` binary; the nucleus pretends to be a tiny Linux and answers the Go runtime's syscalls. Userland later uses the same Linux ABI via musl. Go never runs interrupt handlers; IRQs become wake-ups (ADR-0005). No allocation in Go on interrupts-off paths.
- **Cross-language contracts** live in `include/burrow/` (`boot_info.h`, `manifest.h`) and are mirrored by hand in `boot/boot_abi.inc` and in `tools/mkimage/manifest.go`. Changing a struct or fixed address means updating all mirrors plus `docs/02-architecture/MEMORY_MAP.md` / `BOOT_PROTOCOL.md`. C side uses `__attribute__((packed))` + `_Static_assert` on offsets; Go has layout tests (`TestManifestLayoutMatchesC`).
- **Disk layout:** LBA 0 stage1, LBA 1 manifest, LBA 2..33 stage2, then payloads named in the manifest (`loader`, `nucleus` = stripped `build/nucleus.disk.elf`; built by `tools/mkimage`).
- **Loader-chosen addresses** live in `loader/layout.h` (nucleus physical window 1-4 MiB, palloc range up to stage2's 1 GiB identity map, `PHYSMAP_BASE`). Addresses inside boot_info stay physical. Stage2 writes boot_info v1 (`BOOTINFO_VERSION_STAGE2`); the loader writes v2 (`BOOTINFO_VERSION`). CRC32 is computed identically in mkimage (Go), stage2 (asm) and loader (C).
- **Testability via interfaces:** C uses small function-pointer "ops" structs (`common/char_sink.h`, `loader/block_device.h`, `pt_ctx` in `common/paging.h`); pure modules (common/*, loader manifest/memmap/elf64/elf_load/palloc/vmsetup/image, nucleus pmm/regions) include neither `io.h` nor `cpu.h` and are host-tested; `UNIT_SRC` in the Makefile lists them. Inline asm lives only in those two headers.
- **`common/`** holds freestanding code used by both the loader and the nucleus (kprintf, lib_string, crc32, serial, char_sink, io.h, cpu.h). The Makefile compiles each `common/*.c` twice (`build/loader/common_*.o`, `build/nucleus/common_*.o`); every `common/*.c` except `serial.c` is also in the host unit-test build. Go: interfaces in `kernel/hal`, fakes in `kernel/haltest`; real hardware only in `kernel/arch/amd64` behind build tag `burrow`, so everything else builds and tests on the host.
- `snippets/` holds compile-checked reference code for future waves (nucleus syscall gate, Go IRQ goroutine, Doom backend); it is not part of the build.

- **Nucleus structure** (`nucleus/main.c` calls each module's init in order, each prints one `[nucleus] ... ok` marker): `boot.c` (boot_info, physmap, modules) → `gdt.c` → `trap.c`/`trap.asm` → `frames.c` (PMM) → `vmm.c` (drops identity map, CR0.WP) → `apic.c` → `time.c` → `fpu.c` → `sched.c` → syscall gate → `irq.c`; then `ktest_run` if a `ktest` boot module exists, else `BURROW:NUCLEUS:OK`. `panic()` in `nucleus/panic.c`; errors are `-errno` with Linux numbers (`nucleus/errno.h`).
- **Trap path:** every vector enters on an IST stack; `trap.asm` copies the `struct trap_frame` onto the current thread's kernel stack (`g_percpu.kstack_*`) so handlers may block or switch threads. Faults that are allowed are listed in the `.fixup` section (`nucleus/probe.asm`: `probe_read64/probe_write64/probe_ud2`); `trap_last` records them. Anything else prints `[trap] #XX at rip=...` plus registers and panics.
- **Go kernel (W6):** `kernel/cmd/kernel` is built as a static `GOOS=linux` binary linked at `0x7e0000000000` (`build/kernel.elf`) and shipped only on `build/go.img` as the `kernel` payload; `disk.img` and ktest images stay nucleus-only (fast). `nucleus/go_load.c` maps it and starts thread `go-m0` from a trap frame (`thread_create_from_frame` -> `trap_resume`); `clone` children start the same way with the parent's registers. Every runtime syscall enters `nucleus/syscall.asm` (ring-0 callers classified by RIP in Go or nucleus text, IRETQ back, only RAX/RCX/R11 change) and `nucleus/shim.c`'s table; handlers live in `shim_proc.c`, `shim_mm.c`, `shim_thread.c`, `shim_poll.c` with Linux numbers in `shim.h`. Unknown numbers print `[shim] UNIMPLEMENTED syscall N` once. Nucleus-private calls are `0x1000+` (`NUCLEUS_ABI.md`), reached from Go with `syscall.RawSyscall` (`kernel/arch/amd64/nucleus.go`). Implement each syscall's Linux *semantics* that the runtime relies on (BUG-0009: `MADV_DONTNEED` must zero; `MAP_FIXED` zeroes; futex timeout 0 = do not wait). VM operations hold the address space's `kmutex` (BUG-0008).
- **Go drivers (W7):** interrupts are wake-ups (ADR-0005): ISA lines come through the 8259 in virtual-wire mode (LAPIC LINT0 = ExtINT, so EOI goes to the 8259); `nucleus/irq.c`'s ISR masks the line, EOIs, marks it pending and wakes the one thread in `irq_wait`; `irq_ack` unmasks. Go side: `amd64.IRQ` (`syscall.Syscall`, so the P is handed off) behind `hal.IRQLine`; `driver.Serve` runs wait → handle → ack on a locked OS thread. Drivers live in `kernel/drivers/*`, call `driver.Register` from `init()` and touch hardware only through `driver.Env`; `driver.StartAll` starts them in name order. Keyboard (`drivers/ps2`) and serial (`uart.Input`) bytes go into one channel read by `kernel/console`. Boot order in `kernel/cmd/kernel/main.go`: banner → `testFaults` (kargs) → selftests → drivers → console; `make go-smoke` ends with the console's `poweroff`. A `kargs` boot module's words become the Go kernel's argv (`nucleus/go_load.c`).
- **Threads:** `schedule()` must update percpu kstack bounds, TSS.RSP0, FS_BASE and FXSAVE state before `switch_to` (PIT-22). Blocking primitives use `thread_block_unlock` (PIT-23). The nucleus's C is `-mgeneral-regs-only`; xmm is only touched from asm.

## Test conventions (`docs/05-testing/TEST_STRATEGY.md`)

- **New pure C module:** add `tests/unit/c/test_<mod>.c`, add the module `.c` to `UNIT_SRC` in the Makefile, and declare + `RUN()` every test in `test_main.c` (no auto-registration).
- **Serial markers:** format `[component] message`, success `BURROW:<NAME>:OK`. Expect files (`tests/smoke/expect/*.txt`) are **append-only**; never weaken one to make a test pass. Smoke fails on any `PANIC`/`FAIL` line or out-of-order marker. `make smoke` checks every file in `SMOKE_EXPECT` against the same boot.
- **ktest:** `int kt_<name>(void)` with `KT_CHECK` in `nucleus/ktest_*.c`, declared in `ktest.h`, registered in the table in `ktest.c`, listed in `tests/ktest/<suite>.txt`, with `[ktest] <name> ok` appended to `tests/ktest/expect/<suite>.txt`. Tests that must crash belong in `run_all.sh` via `with_ktest <name>`. Tests that enable interrupts must leave IF as they found it.
- `kprintf` supports only `%% %c %s %d %i %u %x %X %p`, the `0` flag, width and `l`/`ll`/`z`; there is no `-` flag.
- **QEMU exit codes:** guest writes `0x10` to port `0xF4` → exit 33 (pass); `0x11` → 35 (fail); 124 = timeout = hang bug. Every failure path must write debug-exit `0x11` so failures are instant.
- **Regression cases:** add a mutator in `tests/regression/run_all.sh` with `case_run REG-NNNN-name "<expected text>" 35 mutator`; use `lba_of <img> <name>` rather than hard-coding payload LBAs. Go kernel crash cases use `with_kargs <word>` (handled in `kernel/cmd/kernel/kargs.go`). Every bug gets a test named after its BUG/REG id.
- **Typing into a boot:** `tests/smoke/smoke.sh <img> <expect> <timeout> <keys-file>` (steps `wait`/`type`/`key`, sent with QEMU `sendkey` by `tests/smoke/sendkeys.py`; the monitor socket lives in `/tmp` because WSL's `/mnt/c` has no Unix sockets).
- **Go drivers:** host-test with `haltest` (`FakePorts`, `FakeIRQ`, `Fake8042`) and `kernel/hostsim`; the on-target proof is a marker in `boot_to_go.txt`.
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
