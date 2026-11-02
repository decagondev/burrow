# POSIX / Linux syscall coverage for user programs

The Linux x86-64 system calls user programs may make, how far Burrow implements each, and where it deviates ([ADR-0006](../03-decisions/ADR-0006-linux-abi-userland.md)). Implementation: `kernel/proc/syscalls.go`, `kernel/proc/memcalls.go`. A number not listed here returns `-ENOSYS` and is logged once per boot as `[proc] WARN: pid N (prog): unimplemented syscall NR`: that log is the porting to-do list.

This table is about **user programs**. The Go kernel's own runtime syscalls are answered by the nucleus shim instead ([GO_RUNTIME_HOSTING.md](GO_RUNTIME_HOSTING.md), `tests/regression/census-go1.27.txt`).

Status: **done** = Linux semantics for what programs use; **partial** = works, with the deviation noted; **stub** = accepted, does nothing useful yet.

| Nr | Call | Status | Since | Notes |
|---|---|---|---|---|
| 0 | `read` | partial | W9 | files from the VFS; fds 0-2 (console) read as end of file until the TTY (W10) |
| 1 | `write` | partial | W9 | console and device files; regular files are read-only until W10 (`-EBADF`) |
| 2 | `open` | partial | W9 | read-only (`O_WRONLY`/`O_RDWR` → `-EROFS`); relative paths start at `/` (no cwd yet) |
| 3 | `close` | done | W9 | |
| 5 | `fstat` | partial | W9 | `st_mode`, `st_size`, `st_mtim`, `st_nlink`, `st_blksize`; no inode or device numbers |
| 8 | `lseek` | done | W9 | `-ESPIPE` on the console |
| 9 | `mmap` | partial | W9 | anonymous private memory only (`-ENODEV` for files; `/dev/fb0` in W11); `PROT_NONE` pages are readable; a hint without `MAP_FIXED` is ignored |
| 11 | `munmap` | done | W9 | |
| 12 | `brk` | done | W9 | heap after the highest segment |
| 13 | `rt_sigaction` | stub | W9 | returns 0; signals arrive in W10 |
| 14 | `rt_sigprocmask` | stub | W9 | returns 0 |
| 16 | `ioctl` | stub | W9 | `-ENOTTY` for everything (so musl buffers stdout and flushes at exit); TTY ioctls in W10 |
| 20 | `writev` | done | W9 | up to 1024 vectors, 1 MiB in total |
| 35 | `nanosleep` | partial | W9 | never interrupted (no signals) |
| 39 | `getpid` | done | W9 | |
| 56 | `clone` | partial | W10 | only the fork shape (exit signal, `CLONE_VFORK`); `CLONE_VM`/threads: W12 (`-ENOSYS`) |
| 57 | `fork` | partial | W10 | memory copied eagerly (copy-on-write later); descriptors share their open file descriptions |
| 58 | `vfork` | partial | W10 | a full fork (the parent is not suspended) |
| 59 | `execve` | partial | W10 | static ELF only (`-ENOEXEC` otherwise, no `#!` scripts yet); descriptors stay open except `FD_CLOEXEC` |
| 60 | `exit` | partial | W9 | one thread per process, so the same as `exit_group` |
| 61 | `wait4` | partial | W10 | pid > 0, -1, 0, < -1; `WNOHANG`; Linux status words; `rusage` is not filled |
| 110 | `getppid` | done | W10 | orphans report 0 (the kernel) until init adopts them (W10-S07) |
| 102, 104, 107, 108 | `getuid` `getgid` `geteuid` `getegid` | partial | W9 | everyone is root (0) |
| 158 | `arch_prctl` | partial | W9 | `ARCH_SET_FS`, `ARCH_GET_FS` (TLS); not GS |
| 186 | `gettid` | partial | W9 | = pid (one thread per process) |
| 218 | `set_tid_address` | partial | W9 | returns the tid; the clear-on-exit address is ignored (no threads yet) |
| 228 | `clock_gettime` | partial | W9 | every clock is time since the kernel started (no RTC driver) |
| 231 | `exit_group` | done | W9 | |
| 257 | `openat` | partial | W9 | `AT_FDCWD` or an absolute path |

**Faults.** An exception in ring 3 ends the process with the status a Linux shell would show for the matching signal: 139 (SIGSEGV) for page faults and general protection faults, 132 (SIGILL) for invalid opcodes, 136 (SIGFPE) for divide errors. The kernel logs `pid N (prog) killed: page fault at 0x... (rip 0x...)`. Signal delivery to handlers comes with W10.
