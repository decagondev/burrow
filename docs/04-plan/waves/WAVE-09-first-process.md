# W9 First Process  (M9)  — status: in progress

Goal: a static Linux x86-64 program runs in ring 3 under the Go kernel. Demo: `run /bin/hello` prints from ring 3 and exits; `run /bin/crash` dereferences NULL and only that process dies. Stories: E08.F01-F06 ([BACKLOG.md](../BACKLOG.md)), [ADR-0006](../../03-decisions/ADR-0006-linux-abi-userland.md).

Design (refines the provisional ARCHITECTURE §7.3 and ADR-0004):
- **Mechanism vs policy.** The nucleus owns page tables, user threads and the trap/syscall paths; the Go kernel owns processes: which pages exist, ELF loading, the initial stack, every user syscall. Go reads and writes user memory through the physmap (it allocated the frames), never by switching CR3.
- **One CR3 per process, Go never runs on it.** A user thread runs on its process's PML4 (the upper half copied from the kernel's: nucleus, physmap, kernel stacks). Go threads and the nucleus's own threads always run on the kernel's CR3, so the gate classifies a SYSCALL by **CR3** first: not the kernel's = a user caller (sound even if a process maps code at the Go kernel's addresses). Ring-0 callers keep the RIP check.
- **User syscalls are requests.** The gate saves the user registers on the thread's kernel stack, queues `{tid, nr, args}` and blocks the thread. A Go dispatcher goroutine takes requests with `nu_syscall_wait` (a copy out of the nucleus queue, not shared memory), handles each in its own goroutine and answers with `nu_syscall_reply`; the thread returns with `SYSRETQ`. A return RIP that is not canonical (PIT-24) is turned into a fault before SYSRET can #GP in ring 0.
- **Faults are requests too.** An exception in ring 3 queues a fault request; Go kills the process (`nu_uthread_kill`, which also stops a thread preempted in ring 3).

### [x] W9-S01 Address spaces in the nucleus (E08.F01)
`nucleus/uspace.c`: `nu_as_create/map/unmap/translate/destroy`, `nu_page_alloc/free`; per-thread CR3, switched by `schedule()`. ktests: map, translate, unmap, destroy frees the tables, the kernel half is shared.

### [ ] W9-S02 Ring 3: user threads, the user syscall path, faults (E08.F03, E08.F04)
`nu_uthread_create/kill/set_fs`, `nu_syscall_wait/reply`; the gate's user path (CR3 test, kernel stack, SYSRETQ), user exceptions to fault requests, PIT-24. ktests run hand-assembled ring-3 code: getpid + exit through the queue, a NULL read and a kernel-address read become faults, a spinning thread is preempted and killed, the non-canonical return.

### [ ] W9-S03 Processes in Go: address space policy, ELF loader, initial stack (E08.F01, E08.F02)
`kernel/proc`: a process's pages (VMAs over frames from `nu_page_alloc`), copyin/copyout through the physmap, `debug/elf` loading of static `ET_EXEC` (static-PIE later), the Linux entry stack (argv, envp, auxv; the Go twin of `nucleus/go_stack.c`). Host-tested against a fake nucleus (memory-backed frames).

### [ ] W9-S04 Syscall dispatcher and the first syscalls (E08.F04, E08.F05, E08.F06)
Table-driven, unknown numbers logged once and `-ENOSYS`: `write writev read open/openat close lseek fstat brk mmap(anon) munmap exit exit_group getpid gettid set_tid_address arch_prctl ioctl(TCGETS/TIOCGWINSZ: -ENOTTY) rt_sigprocmask(stub) clock_gettime nanosleep`. `docs/02-architecture/POSIX_COVERAGE.md`.

### [ ] W9-S05 First programs: asm hello and crash (E08.F03)
`user/` builds static programs with nasm + ld into the initrd's `/bin`; console `run <path> [args]`. go-smoke runs `/bin/hello` (prints, exit status 0) and `/bin/crash` (killed with a page fault report, the console carries on).

### [ ] W9-S06 musl (E08.F05)
`user/hello.c` built with `musl-gcc -static` (CI installs musl-tools); go-smoke runs it.

Exit: **M9 demo — first process.** `make clean test` green; DEVLOG wave entry.
