# ADR-0006: Linux x86-64 syscall ABI + musl for user space

- **Status:** Accepted
- **Date:** 2026-11-02
- **Wave:** W9

## Context
Milestones include Doom, Lua, C++, Go programs, and porting other projects. Inventing a new ABI means porting a libc and every runtime.

## Options
1. Custom ABI + own libc (newlib port). Classic hobby-OS route; lots of porting.
2. **Linux syscall numbers and struct layouts (subset) + musl.** Static Linux binaries can run unmodified once their syscalls exist. Go user programs (`GOOS=linux`) work for free.

## Decision
Option 2, implemented incrementally. Unknown syscalls log once and return `-ENOSYS`, which turns porting into a discovery list.

## Consequences
- We follow Linux semantics where we implement something; deviations are documented in `docs/02-architecture/POSIX_COVERAGE.md` (created in W9).
- fbdev and evdev subsets give Doom and other software a known device API.
- "It works on Linux" becomes a powerful debugging oracle: run the same binary on Linux with strace and compare.
