# ADR-0003: Host the stock Go runtime on a Linux-syscall shim

- **Status:** Accepted (confirmed by spike W6-S01 with the pinned Go version)
- **Date:** 2026-11-02
- **Wave:** W6

## Context
The Go runtime needs threads, memory, time, locks and output. On Linux it gets them via ~22 raw syscalls (census). We must provide them in ring 0.

## Options
1. **Fork/modify the runtime** (Biscuit). Full control; permanent maintenance of a fork; locked to one Go version.
2. **TamaGo `GOOS=tamago`.** Maintained, minimal patch, clean hooks. Needs a custom toolchain; single-threaded runtime model; user space still needs its own ABI.
3. **Stock `GOOS=linux` binary on a nucleus that implements the Linux syscall subset** (eggos-style, but with a C nucleus).
4. **Wait for upstream `GOOS=none`.** Not available today.

## Decision
Option 3. The same Linux ABI knowledge is reused for user processes (ADR-0006), the toolchain is stock, and the C nucleus keeps Go out of interrupt context.

## Consequences
- We must pin the Go toolchain and re-run the syscall census on upgrade (PIT-09, regression test REG-census in W6).
- The nucleus must implement threads and futexes *before* Go can run (Wave 5 is on the critical path).
- Plan B: if W6 stalls for more than two waves on runtime incompatibilities, switch the kernel build to TamaGo amd64; nucleus and HAL interfaces are unaffected.
