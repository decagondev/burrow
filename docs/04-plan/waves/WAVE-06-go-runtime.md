# W6 Go Says Hello  (M6)  — status: planned

Goal: an unmodified `GOOS=linux` Go binary runs in ring 0 on the nucleus. Contract: [GO_RUNTIME_HOSTING.md](../../02-architecture/GO_RUNTIME_HOSTING.md).

### [x] W6-S01 [spike] Confirm the hosting model with the pinned Go (E05.F01)
Install Go 1.27.x. Re-run the census (`tools/census`), the `-T 0x7e0000000000` link test, and a test where `mmap` without a hint returns addresses in `0x7D00_0000_0000+` (run under a small `LD_PRELOAD`-free ptrace or seccomp experiment, or just test on the nucleus in S08). Output: updated census doc; ADR-0003 status; ADR-0004 address.
Timebox: 2 sessions. Failure → plan B review.

### [x] W6-S02 Stage the kernel ELF (E05.F02)
Makefile: build `kernel.elf` (command in GO_RUNTIME_HOSTING §2), `-payload kernel=`. Loader loads it as a module (not segments: the nucleus maps it).

### [ ] W6-S03 Syscall gate (E05.F03)
Copy `snippets/nucleus/syscall_gate.asm`. MSRs: EFER.SCE, STAR (`kernel CS 0x08 in 47:32`, `user base 0x18 in 63:48`), LSTAR, FMASK (clear IF, DF, TF). ktest: a C function issuing `syscall` with nr 39 returns 1.

### [ ] W6-S04 Shim skeleton + census-on-failure (E05.F03)
Copy `syscall_shim.c` with only `write` and the unknown-logger. ktest: unknown nr prints `[shim] UNIMPLEMENTED syscall 999` once.

### [ ] W6-S05 Map the Go ELF + initial stack (E05.F02)
Map PT_LOAD supervisor pages; record text range; `build_initial_stack()`; create thread 1; enter. Expected first result: the census logger names the first syscall the runtime needs. That is success for this slice.

### [ ] W6-S06 arch_prctl, signals stubs, fcntl, openat, getrlimit, affinity (E05.F04)
Iterate: boot, read the UNIMPLEMENTED line, implement, repeat. Log each in DEVLOG.

### [ ] W6-S07 mmap/munmap/madvise on reserve/commit (E05.F04)
Honour free hints; otherwise allocate from `0x7D00_0000_0000`. Test: runtime reaches `schedinit` (gdb breakpoint `runtime.schedinit`).

### [ ] W6-S08 clone (E05.F04)
`thread_clone(rip, rsp, tls, rflags)`; child returns 0 from the gate. Test: sysmon thread appears in the nucleus thread list (`[sched] threads=2+`).

### [ ] W6-S09 futex, nanosleep, sched_yield, clock_gettime, gettid (E05.F04)
Test: `main()` starts; `BURROW:GO:OK goroutine says 42`.

### [ ] W6-S10 Netpoller minimum: pipe2 + epoll (E05.F04)
One wake pipe object; `epoll_pwait` = futex-style wait on (pipe written OR timeout). Test: `time.Sleep(100ms)` x 10 within 10%.

### [ ] W6-S11 Selftest package (E05.F05)
`kernel/selftest`: goroutine ping-pong 100k; GC stress allocating 10x RAM in a sliding window; map/slice/interface/closure smoke. Markers `[go-selftest] ... ok`.

### [ ] W6-S12 Census regression (E00.F02.S05)
`tests/regression/census.sh`: run `tools/census` on the host under strace; diff the syscall set against `tests/regression/census-go1.27.txt`; new syscalls fail the test with a pointer to the shim.

Exit: **M6 demo — Go says hello from ring 0.** Write a long DEVLOG entry; this is the project's biggest milestone before Doom.
