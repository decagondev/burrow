# Research: Operating Systems Written in Go (survey, Nov 2026)

Purpose: learn from every serious attempt before choosing our design. Each project gets: what it is, how it hosts the Go runtime, what we take, what we avoid. Sources are listed at the end; re-check them when you start a wave, since projects move.

## 1. Comparison

| Project | Kind | Arch | Runtime strategy | Processes / POSIX | Status (as researched) |
|---|---|---|---|---|---|
| **Biscuit** (MIT PDOS) | monolithic research kernel | x86-64 | modified runtime, hijacks `GOOS=linux` | yes: ran NGINX, Redis unmodified | research artifact (OSDI'18) |
| **eggos** | unikernel | x86 (32-bit focus) | stock runtime in ring 0, runtime syscalls trapped by the kernel | no processes, goroutines only | dormant; pinned old Go (1.16) |
| **gopher-os** | experimental kernel | x86-64 | own rt0 sets up `g0`/FS, calls into runtime pieces | goal of Linux-compatible syscalls | proof of concept, inactive |
| **TamaGo** (WithSecure) | bare-metal Go framework | amd64, ARM, ARM64, RISC-V | patched toolchain, new `GOOS=tamago`, small hook set | single app, no processes | active, maintained |
| **GOOS=none proposal** (golang/go #73608) | upstream proposal | multiple | standard hooks for freestanding Go, derived from TamaGo | n/a | proposal |
| **TinyGo** | alternative compiler | MCUs, wasm | own small runtime | n/a | active; good for embedded, not our kernel |
| **gVisor** (Google) | user-space kernel | x86-64, ARM64 | normal Go program on Linux | implements Linux syscall ABI in Go | production; a goldmine of Go POSIX logic and netstack |

## 2. Lessons per project

**Biscuit.** Proves a Go POSIX kernel can be fast enough: HLL costs up to ~13-15% CPU, GC pauses ~100 µs, Go vs C hot paths 5-15% slower. Its hardest problem was kernel heap exhaustion, because Go cannot report allocation failure; Biscuit reserved memory per syscall before running it, using static analysis. *We take:* the overall feasibility, the per-operation memory-reservation idea (risk R-03), the monolithic POSIX feature list as a checklist. *We avoid:* forking the runtime; it ties you to one Go version forever.

**eggos.** Runs an *unmodified* Go program in ring 0 and services the runtime's Linux syscalls inside the kernel. This is the ancestor of our nucleus shim. Its author notes interrupt handling and syscalls are where Go's implicit scheduling and GC bite. Its README warns newer Go versions may not work. *We take:* the "pretend to be Linux" hosting model. *We avoid:* letting Go code run inside interrupt handlers (we deliver wake-ups instead), and leaving the Go version unpinned without a census regression test.

**gopher-os.** Shows the low-level mechanics clearly: a 32-bit rt0 that builds early page tables, relocates to `0xffff800000000000`, sets FS to a hand-built `g0`, and calls Go. It historically linked through `go build -n` and external `ld`, which broke as Go evolved. *We take:* its commit history as a step-by-step tutorial on rt0. *We avoid:* depending on Go's internal build steps.

**TamaGo.** The cleanest freestanding approach: a minimal, maintained toolchain patch plus hooks (`cpuinit`, `hwinit`, `printk`, `initRNG`, `nanotime1`, RAM layout). Interrupts wake a parked goroutine via `runtime.WakeG`. Supports amd64 under KVM/QEMU microvm and UEFI. *We take:* the "IRQ wakes a goroutine" model (ADR-0005), and TamaGo is our **plan B** if the Linux-shim approach fails (ADR-0003). *We avoid (for now):* a custom toolchain, because our user-space story wants the Linux ABI anyway.

**gVisor.** Not bare metal, but it is a large, well-tested Go implementation of Linux syscall semantics, VFS, and a TCP/IP stack (`netstack`). *We take:* a reference for POSIX edge cases in Wave 10, and its netstack is a candidate for networking (E12).

**doomgeneric.** Doom needs five functions from the platform (`DG_Init`, `DG_DrawFrame`, `DG_SleepMs`, `DG_GetTicksMs`, `DG_GetKey`). Many hobby OSes use it. *We take:* a backend written against Linux fbdev + evdev so it runs on Linux first (`snippets/userland/doomgeneric_burrow.c`). Use **Freedoom** WADs (BSD licence) inside the repo/initrd; the shareware `doom1.wad` is freely available but keep it out of the repo.

## 3. Decisions this research drove

| Finding | Decision |
|---|---|
| Forked runtimes rot (Biscuit, gopher-os, eggos pinned to old Go) | Stock toolchain + Linux syscall shim; pin version; census regression ([ADR-0003](../03-decisions/ADR-0003-go-runtime-hosting.md)) |
| Go in interrupt context is the top source of bugs (eggos) | Interrupts never run Go; they wake threads ([ADR-0005](../03-decisions/ADR-0005-irq-as-wakeups.md)) |
| Users want existing software (Doom, Lua, C++) | Linux x86-64 syscall ABI for user space, musl libc ([ADR-0006](../03-decisions/ADR-0006-linux-abi-userland.md)) |
| Allocation failure is unrecoverable in Go (Biscuit) | Heap budget + reserve; risk R-03; operations reserve before running (later) |
| Learning value of seeing each step (gopher-os history) | Own bootloader, own loader, tiny slices ([ADR-0002](../03-decisions/ADR-0002-own-bios-bootloader.md)) |

## 4. Our own experiments

See [GO_RUNTIME_SYSCALL_CENSUS.md](GO_RUNTIME_SYSCALL_CENSUS.md): the runtime syscall set, the `PROT_NONE` reservation discovery, and the high text-address link test.

## 5. Sources

- Cutler, Kaashoek, Morris, *The benefits and costs of writing a POSIX kernel in a high-level language*, OSDI 2018 — https://www.usenix.org/conference/osdi18/presentation/cutler ; code: https://github.com/mit-pdos/biscuit
- eggos — https://github.com/icexin/eggos
- gopher-os — https://github.com/achilleasa/gopher-os
- TamaGo — https://github.com/usbarmory/tamago (wiki: Internals, FAQ); toolchain https://github.com/usbarmory/tamago-go
- Go proposal for freestanding hooks — https://github.com/golang/go/issues/73608
- Go runtime Linux syscalls — `src/runtime/sys_linux_amd64.s`, `src/runtime/os_linux.go`
- doomgeneric — https://github.com/ozkl/doomgeneric ; Freedoom — https://freedoom.github.io
- gVisor — https://github.com/google/gvisor
- Go 1.27 release notes — https://go.dev/doc/go1.27
- OSDev wiki (reference for every hardware detail) — https://wiki.osdev.org
