# POSIX / Linux syscall coverage for user programs

The Linux x86-64 system calls user programs may make, how far Burrow implements each, and where it deviates ([ADR-0006](../03-decisions/ADR-0006-linux-abi-userland.md)). Implementation: `kernel/proc/syscalls.go`, `kernel/proc/memcalls.go`. A number not listed here returns `-ENOSYS` and is logged once per boot as `[proc] WARN: pid N (prog): unimplemented syscall NR`: that log is the porting to-do list.

This table is about **user programs**. The Go kernel's own runtime syscalls are answered by the nucleus shim instead ([GO_RUNTIME_HOSTING.md](GO_RUNTIME_HOSTING.md), `tests/regression/census-go1.27.txt`).

Status: **done** = Linux semantics for what programs use; **partial** = works, with the deviation noted; **stub** = accepted, does nothing useful yet.

| Nr | Call | Status | Since | Notes |
|---|---|---|---|---|
| 0 | `read` | partial | W9 | files, pipes (blocking, `EINTR` with signals), devices; fds 0-2 read the console (TTY: W10-S05) |
| 1 | `write` | partial | W9 | console, pipes (`EPIPE`), devices, writable files under `/tmp` (written back whole on every write) |
| 2 | `open` | partial | W9 | relative to the cwd; `O_CREAT O_EXCL O_TRUNC O_APPEND O_DIRECTORY O_CLOEXEC`; writes only where the mount is writable (`/tmp`: `-EACCES` on the read-only root) |
| 3 | `close` | done | W9 |  |
| 4, 6 | `stat` `lstat` | partial | W10 | no symbolic links, so both follow; `st_ino` is a hash of the path |
| 5 | `fstat` | partial | W9 | `st_mode`, `st_size`, times, `st_nlink` 1, `st_blksize`, `st_blocks` |
| 8 | `lseek` | done | W9 | `-ESPIPE` on the console and pipes |
| 9 | `mmap` | partial | W9 | anonymous private memory only (`-ENODEV` for files; `/dev/fb0` in W11); `PROT_NONE` pages are readable; a hint without `MAP_FIXED` is ignored |
| 11 | `munmap` | done | W9 |  |
| 12 | `brk` | done | W9 | heap after the highest segment |
| 13 | `rt_sigaction` | stub | W9 | returns 0; signals arrive in W10-S04 |
| 14 | `rt_sigprocmask` | stub | W9 | returns 0 |
| 16 | `ioctl` | stub | W9 | `-ENOTTY` for everything (so musl buffers stdout and flushes at exit); TTY ioctls in W10-S05 |
| 20 | `writev` | done | W9 | up to 1024 vectors, 1 MiB in total |
| 21 | `access` | partial | W10 | existence only (everyone is root) |
| 22 | `pipe` | done | W10 | 64 KiB buffer |
| 32, 33 | `dup` `dup2` | done | W10 |  |
| 35 | `nanosleep` | partial | W9 | never interrupted (no signals yet) |
| 39 | `getpid` | done | W9 |  |
| 56 | `clone` | partial | W10 | only the fork shape (exit signal, `CLONE_VFORK`); `CLONE_VM`/threads: W12 (`-ENOSYS`) |
| 57 | `fork` | partial | W10 | memory copied eagerly (copy-on-write later); descriptors share their open file descriptions |
| 58 | `vfork` | partial | W10 | a full fork (the parent is not suspended) |
| 59 | `execve` | partial | W10 | static ELF only (`-ENOEXEC` otherwise, no `#!` scripts yet); descriptors stay open except `FD_CLOEXEC` |
| 60 | `exit` | partial | W9 | one thread per process, so the same as `exit_group` |
| 61 | `wait4` | partial | W10 | pid > 0, -1, 0, < -1; `WNOHANG`; Linux status words; `rusage` is not filled |
| 63 | `uname` | done | W10 | `Burrow burrow 0.10 x86_64` |
| 72 | `fcntl` | partial | W10 | `F_DUPFD(_CLOEXEC) F_GETFD F_SETFD F_GETFL F_SETFL` (`O_APPEND`, `O_NONBLOCK` stored, not yet honoured); no locks |
| 79 | `getcwd` | done | W10 |  |
| 80, 81 | `chdir` `fchdir` | done | W10 |  |
| 83, 84 | `mkdir` `rmdir` | done | W10 | writable mounts only; `-ENOTEMPTY` |
| 87 | `unlink` | done | W10 | `-EISDIR` for directories |
| 102, 104, 107, 108 | `getuid` `getgid` `geteuid` `getegid` | partial | W9 | everyone is root (0) |
| 110 | `getppid` | done | W10 | orphans report 0 (the kernel) until init adopts them (W10-S07) |
| 158 | `arch_prctl` | partial | W9 | `ARCH_SET_FS`, `ARCH_GET_FS` (TLS); not GS |
| 186 | `gettid` | partial | W9 | = pid (one thread per process) |
| 217 | `getdents64` | done | W10 | includes `.` and `..`; `d_ino` made up |
| 218 | `set_tid_address` | partial | W9 | returns the tid; the clear-on-exit address is ignored (no threads yet) |
| 228 | `clock_gettime` | partial | W9 | every clock is time since the kernel started (no RTC driver) |
| 231 | `exit_group` | done | W9 |  |
| 257 | `openat` | done | W10 | `AT_FDCWD` or a directory fd |
| 258, 263 | `mkdirat` `unlinkat` | done | W10 | `AT_REMOVEDIR` |
| 262 | `newfstatat` | done | W10 | `AT_EMPTY_PATH` |
| 269, 439 | `faccessat` `faccessat2` | partial | W10 | existence only |
| 292 | `dup3` | done | W10 | `O_CLOEXEC` |
| 293 | `pipe2` | partial | W10 | `O_CLOEXEC`; `O_NONBLOCK` not honoured |

**Faults.** An exception in ring 3 ends the process with the status a Linux shell would show for the matching signal: 139 (SIGSEGV) for page faults and general protection faults, 132 (SIGILL) for invalid opcodes, 136 (SIGFPE) for divide errors. The kernel logs `pid N (prog) killed: page fault at 0x... (rip 0x...)`. Signal delivery to handlers comes with W10.
