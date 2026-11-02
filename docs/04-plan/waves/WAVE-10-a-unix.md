# W10 A Unix  (M10)  — status: in progress

Goal: processes behave like Unix processes: `fork`, `execve`, `wait4`, pipes and `dup`, a working directory, signals with handlers, and a terminal with a line discipline where `^C` interrupts the foreground job. Demo: a shell at the console runs `ls / | wc -l`, and `^C` stops a busy loop. Stories: E09.F01-F05 ([BACKLOG.md](../BACKLOG.md)).

Design (continues W9: mechanism in the nucleus, policy in Go):
- **Whole register state.** Go must read and rewrite a stopped user thread's registers for fork (copy them), execve (new RIP/RSP/address space) and signals (enter a handler, `rt_sigreturn`). The user syscall path now saves a full `struct trap_frame` (plus the thread's FXSAVE area) and returns through `trap_resume` (IRETQ) like an interrupt; `nu_uthread_regs` reads and writes it while the thread waits. Every register Go writes is checked: user RIP/RSP, RFLAGS limited to the user-settable bits with IF on, MXCSR reserved bits clear. SYSRETQ can come back later as a fast path for plain returns.
- **Requests can resume.** A fault request answered by Go resumes the faulting instruction (for copy-on-write later, and for signal handlers); a killed thread still leaves.
- **Asynchronous stops.** `nu_uthread_interrupt` makes a thread running in ring 3 stop at its next pass through the kernel (a timer tick at the latest) and post an *interrupt* request, so Go can deliver a signal to a process that makes no system calls (`^C` on a busy loop).
- **fork copies eagerly** in this wave (every page duplicated); copy-on-write needs only the resumable faults above and is a later optimisation.
- **Userland is ours first.** A small musl shell (`user/sh.c`: pipelines, `cd`, `exit`, foreground jobs) and tools (`ls cat echo wc spin kill`) prove the kernel. busybox needs its source downloaded and built; that is S08, behind an explicit go-ahead.

### [x] W10-S01 Nucleus: full frames, register access, resumable and asynchronous requests (E09.F01, E09.F03)
User syscalls build a `trap_frame` and return with IRETQ. New calls: `nu_uthread_regs` (get/set), `nu_uthread_create_from` (a thread from a register image: fork), `nu_uthread_set_space` (execve), `nu_uthread_interrupt`. Fault requests resume when answered. ktests for each.

### [x] W10-S02 fork, execve, wait4, exit status (E09.F01)
`kernel/proc`: a process tree (pid, ppid, pgid, sid), `fork` (eager copy of memory and descriptors), `execve` (new image, close-on-exec), `wait4`/`waitpid` with `WNOHANG` and Linux status encoding, zombies, orphans reparented to init, `getppid`, `exit` of the last thread.

### [x] W10-S03 File descriptors and the working directory (E09.F02)
`dup dup2 dup3 pipe pipe2 fcntl(F_DUPFD, F_GETFD/SETFD, F_GETFL/SETFL)`, `O_CLOEXEC`, `getcwd chdir fchdir`, the `*at` family relative to the cwd, `stat lstat newfstatat getdents64 access`, writable files under `/tmp` (`O_CREAT O_TRUNC O_APPEND`, `unlink`, `mkdir`), `uname`.

### [ ] W10-S04 Signals (E09.F03)
`rt_sigaction rt_sigprocmask rt_sigreturn kill tgkill`, default actions (terminate, ignore, stop later), Linux `rt_sigframe` with the FPU state, delivery when a call returns and asynchronously through `nu_uthread_interrupt`, `EINTR` for interrupted blocking calls, `SIGCHLD`, faults as `SIGSEGV`/`SIGILL`/`SIGFPE` to a handler if one is installed.

### [ ] W10-S05 TTY line discipline (E09.F04)
`kernel/tty`: canonical mode (echo, erase, kill, `^D`), `^C`/`^\` to the foreground process group, raw mode through `termios` (`TCGETS TCSETS* TIOCGWINSZ TIOCGPGRP TIOCSPGRP TIOCSCTTY`), `setpgid getpgid setsid getsid`. The console's `sh` command gives the terminal to a shell until it exits.

### [ ] W10-S06 Userland: sh and tools (E09.F05)
`user/sh.c` and the tools, built with musl into `/bin`; go-smoke types `sh`, then `ls / | wc -l`, `echo hi > /tmp/x; cat /tmp/x`, `spin` + `^C`, `exit`.

### [ ] W10-S07 init in Go (E09.F05)
PID 1 is a Go-side init: it adopts orphans and reaps zombies; the console's shell runs as its child.

### [ ] W10-S08 busybox (E09.F05) — needs a go-ahead to download its source
Build busybox (static, musl) from its source release; run `busybox sh` and its applets.

Exit: **M10 demo — a Unix.** `make clean test` green; DEVLOG wave entry.
