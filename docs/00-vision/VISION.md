# Vision

**Burrow** (placeholder name; rename freely) is an educational x86-64 operating system whose kernel is written in Go, booted by our own assembly and C, designed to grow into a POSIX-like system that runs real software, with **DOOM** as the flagship milestone.

## Goals
1. **Understandable.** Every step is a small slice a person can type by hand and test in minutes.
2. **Honest engineering.** Decisions are written down (ADRs), bugs are logged with mitigations, tests guard every layer.
3. **Modular (SOLID).** Small interfaces, pure cores, thin hardware shells; new drivers, filesystems and boot paths plug in without rewrites.
4. **Real software.** Linux-compatible syscall subset so that musl-based C, C++, Lua and Go programs port easily.
5. **Milestones you can show:** boot → long mode → C → Go hello → keyboard → files → processes → shell → DOOM → ports.

## Non-goals (for now)
- Performance parity with Linux (Biscuit shows ~10-15% overhead is realistic; fine).
- Security hardening beyond basic isolation (E12).
- Binary compatibility with all of Linux; we implement what our ports need, and document gaps.
- Supporting 32-bit CPUs or non-x86 architectures.

## Principles
- Contracts before code. Tests before features. Verify before execute.
- Mechanism in C, policy in Go.
- Prefer boring, well-documented hardware (serial, ATA PIO, PS/2, BGA) until a milestone needs more.
- Everything works in QEMU first.
