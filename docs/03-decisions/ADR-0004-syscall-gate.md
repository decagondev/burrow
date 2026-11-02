# ADR-0004: One SYSCALL gate, caller classified by RIP range

- **Status:** Proposed (accept after W6-S03 and W9 tests)
- **Date:** 2026-11-02
- **Wave:** W6, W9

## Context
Both the ring-0 Go runtime and ring-3 user programs execute `SYSCALL`, which lands at a single `IA32_LSTAR` entry. After `SYSCALL`, CS is always the kernel's, so the CPL of the caller is lost. We must know whether to `swapgs`, which stack to use, and how to return.

## Options
1. Check the caller's GS base value (MSR read on every syscall).
2. Per-thread "is user" flag (needs GS to find the thread: chicken and egg before swapgs).
3. **Classify by return RIP (RCX):** inside Go kernel text => Go; otherwise user.
4. Different instruction for Go (e.g. `INT 0x80`) — requires patching the runtime.

## Decision
Option 3. Go kernel text is mapped supervisor-only in every address space, so user code can never execute there and therefore can never produce such an RCX.

## Consequences
- The Go kernel text range must be known before the first syscall (recorded at ELF load).
- Return to Go via `IRETQ` (SYSRET forces CPL 3); return to user via `SYSRETQ` only with canonical RCX, else `IRETQ`.
- If the spike forces the Go kernel to a different address, only `go_text_lo/hi` change.
