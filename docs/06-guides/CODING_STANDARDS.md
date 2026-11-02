# Coding Standards

## General
- One file, one responsibility. If a file needs "and" to describe it, split it.
- Prefer < 300 lines per file and < 50 lines per function. Assembly routines < 40 instructions where possible.
- Every file starts with a comment: what it does, what it must never do, which doc/ADR explains it.
- Names say units: `size_bytes`, `count_sectors`, `timeout_ns`, `phys`/`virt` prefixes for addresses.
- No magic numbers outside one constants place per component (`boot_abi.inc`, headers, Go `const` blocks).

## SOLID per language

| Principle | Assembly | C | Go |
|---|---|---|---|
| SRP | one routine per task, one stage per job | one module per `.c` | one package per concept |
| OCP | append markers/routines | tables of function pointers, registries | interfaces + `init()` registration |
| LSP | — | fakes obey the same contract | fakes in `haltest`, conformance tests |
| ISP | tiny include files (`lib16.inc`) | 1-3 function "ops" structs | 1-3 method interfaces |
| DIP | — | depend on `char_sink`, `block_device` | depend on `hal`, inject in `main` |

## Assembly
- NASM syntax; `bits` and `org` at top; constants from `boot_abi.inc`.
- Document clobbered registers in the routine header.
- Label locals with `.name`.

## C
- C11, freestanding, `-Wall -Wextra -Werror`.
- No mutable initialised globals in the loader (self-CRC; BUG-0003 context).
- Fixed-width types from `<stdint.h>` for anything that crosses an ABI.
- Every cross-language struct: `__attribute__((packed))` + `_Static_assert` on offsets.
- Hardware access only through `io.h`-style headers.

## Go
- `gofmt`, `go vet` clean.
- Hardware only in `arch/amd64` behind build tag `burrow`; everything else must build and test on the host.
- No allocation in code paths documented as "may run with interrupts off" (there should be none in Go; ADR-0005).
- Errors are values; `panic` only for kernel invariants.

## Commits
- One slice per commit: `W4-S03: load manifest entry with CRC check`.
- Bug fixes: `BUG-0007: fix ...` and the regression test in the same commit.
