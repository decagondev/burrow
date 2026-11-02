# ADR-0004: One SYSCALL gate, caller classified by RIP range

- **Status:** Accepted, amended in W9 (see "W9 amendment" below)
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

## W9 amendment: classify by CR3 first
Building processes showed option 3 alone is not sound once each process has its own page tables: the Go kernel text is mapped only in the kernel's address space, so a process could map its own code at those addresses and pass the RIP test. The gate now asks first whether **CR3 is the kernel's**: Go and the nucleus always run on it, user threads never do (each runs on its process's PML4). Not the kernel's CR3 = a user caller, whatever RCX says; on the kernel's CR3 the RIP test still separates Go text and nucleus text from a stray caller. The CR3 read costs one `mov` and needs one scratch slot (IF is off on entry). The user return is `SYSRETQ`; a non-canonical return RIP (PIT-24) is turned into a fault in C before the return, because the IRETQ fallback would #GP in ring 0 just the same. Tests: `tests/ktest/user.txt` (`uthread_*`).
