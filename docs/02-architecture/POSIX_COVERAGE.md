# POSIX / Linux syscall coverage for user programs

The Linux x86-64 system calls user programs may make, how far Burrow implements each, and where it deviates ([ADR-0006](../03-decisions/ADR-0006-linux-abi-userland.md)). Implementation: `kernel/proc/syscalls.go`, `kernel/proc/memcalls.go`. A number not listed here returns `-ENOSYS` and is logged once per boot as `[proc] WARN: pid N (prog): unimplemented syscall NR`: that log is the porting to-do list.

This table is about **user programs**. The Go kernel's own runtime syscalls are answered by the nucleus shim instead ([GO_RUNTIME_HOSTING.md](GO_RUNTIME_HOSTING.md), `tests/regression/census-go1.27.txt`).

Status: **done** = Linux semantics for what programs use; **partial** = works, with the deviation noted; **stub** = accepted, does nothing useful yet.

| Nr | Call | Status | Since | Notes |
|---|---|---|---|---|
| 0 | `read` | done | W9 | files, pipes and the terminal (blocking, `EINTR` with signals), devices |
| 1 | `write` | partial | W9 | console, pipes (`EPIPE`), devices, writable files under `/tmp` (written back whole on every write) |
| 2 | `open` | partial | W9 | relative to the cwd; `O_CREAT O_EXCL O_TRUNC O_APPEND O_DIRECTORY O_CLOEXEC O_NONBLOCK` (honoured by pipes, the terminal and devices that report readiness, W11); writes only where the mount is writable (`/tmp`, and `/mnt` or `/home` on a FAT32 disk since W13: `-EACCES` on the read-only root) |
| 3 | `close` | done | W9 |  |
| 4, 6 | `stat` `lstat` | partial | W10 | no symbolic links, so both follow; `st_ino` is a hash of the path |
| 5 | `fstat` | partial | W9 | `st_mode`, `st_size`, times, `st_nlink` 1, `st_blksize`, `st_blocks` |
| 7 | `poll` | done | W10 | the terminal (a poller counts as a reader, so the console forwards keys), pipes (`POLLHUP`, `POLLERR`); regular files and the console are always ready; `POLLNVAL` for a closed fd |
| 8 | `lseek` | done | W9 | `-ESPIPE` on the console and pipes |
| 9 | `mmap` | partial | W9 | anonymous private memory (demand-zero since W12: a reservation costs nothing, a page gets its frame at first touch; `PROT_NONE` is inaccessible), and device memory with `MAP_SHARED` (`/dev/fb0`, W11: shared by fork, never freed; `-EFAULT` if a syscall is handed a pointer into it); regular files privately since W14 (`MAP_PRIVATE`, and read-only `MAP_SHARED`, which is the same thing; any page-aligned offset, `PROT_EXEC`, `MAP_FIXED`; pages fill from the file at first touch, past its end they read as zeros, writes stay in the process and fork copies them; the mapping outlives `close`; `-EACCES` on a write-only descriptor, `-ENODEV` for writable `MAP_SHARED`, pipes, the terminal, directories and devices without memory); `PROT_NONE` pages are readable; a hint without `MAP_FIXED` is ignored |
| 10 | `mprotect` | partial | W12 | pages and areas; pages already touched stay readable under `PROT_NONE` |
| 11 | `munmap` | done | W9 |  |
| 12 | `brk` | done | W9 | heap after the highest segment |
| 13 | `rt_sigaction` | done | W10 | `SA_SIGINFO SA_RESTORER SA_RESTART SA_NODEFER SA_RESETHAND SA_ONSTACK` (W12) |
| 14 | `rt_sigprocmask` | done | W10 | |
| 15 | `rt_sigreturn` | done | W10 | Linux `rt_sigframe`: `ucontext` with `sigcontext` and the FXSAVE state, `siginfo` |
| 16 | `ioctl` | partial | W10 | the terminal: `TCGETS TCSETS TCSETSW TCSETSF TIOCGWINSZ TIOCSWINSZ TIOCGPGRP TIOCSPGRP TIOCSCTTY TIOCNOTTY TIOCGSID FIONREAD`; `/dev/fb0` (W11): `FBIOGET_VSCREENINFO FBIOGET_FSCREENINFO`, `FBIOPUT_VSCREENINFO` and `FBIOPAN_DISPLAY` for the current mode only; `/dev/input/event0` and `event1`: `EVIOCGNAME EVIOCGVERSION EVIOCGID EVIOCGRAB`, and since W14 `EVIOCGBIT EVIOCGKEY EVIOCGPHYS` with empty `EVIOCGPROP EVIOCGLED EVIOCGSND EVIOCGSW` (`EVIOCGUNIQ` is `-ENOENT`); anything else `-ENOTTY` |
| 19 | `readv` | done | W11 | one read of the vectors' total length (at most 1 MiB), scattered over them |
| 20 | `writev` | done | W9 | up to 1024 vectors, 1 MiB in total |
| 21 | `access` | partial | W10 | existence only (everyone is root) |
| 22 | `pipe` | done | W10 | 64 KiB buffer |
| 23 | `select` | partial | W14 | over the same readiness as `poll` (`kernel/proc/select.go`); a hang-up also marks a descriptor in the write set; the time left is not written back into the timeout |
| 24 | `sched_yield` | done | W12 | |
| 28 | `madvise` | partial | W12 | `MADV_DONTNEED` gives frames back (anonymous memory reads as zero, a file mapping as the file again); other advice accepted |
| 32, 33 | `dup` `dup2` | done | W10 |  |
| 34 | `pause` | done | W10 | |
| 35 | `nanosleep` | partial | W10 | `EINTR` when a signal arrives; the remaining time is not written back |
| 39 | `getpid` | done | W9 |  |
| 56 | `clone` | partial | W12 | threads (`CLONE_VM`, `CLONE_FS`, `CLONE_FILES`, `CLONE_SIGHAND` and `CLONE_THREAD` together; `SETTLS PARENT_SETTID CHILD_SETTID CHILD_CLEARTID`) and the fork shape; `CLONE_VM` without `CLONE_THREAD` (vfork, posix_spawn) copies like fork |
| 57 | `fork` | partial | W10 | memory copied eagerly (copy-on-write later); descriptors share their open file descriptions |
| 58 | `vfork` | partial | W10 | a full fork (the parent is not suspended) |
| 59 | `execve` | partial | W10 | static ELF only (`-ENOEXEC` otherwise, no `#!` scripts yet); descriptors stay open except `FD_CLOEXEC` |
| 60 | `exit` | done | W12 | ends the calling thread (clearing and waking its `clear_child_tid`); the last thread ends the process |
| 62 | `kill` | done | W10 | pid > 0, 0, -1, < -1; signal 0 checks existence |
| 61 | `wait4` | partial | W10 | pid > 0, -1, 0, < -1; `WNOHANG`; Linux status words; `rusage` is not filled |
| 74, 75 | `fsync` `fdatasync` | done | W13 | nothing to wait for: a write is on the disk when `write` returns (FAT flushes each change) |
| 76, 77 | `truncate` `ftruncate` | partial | W13 | regular files open for writing (`/tmp`); `-EINVAL` for others, `-EBADF` if not open for writing |
| 82, 264, 316 | `rename` `renameat` `renameat2` | done | W13 | inside one mount (`-EXDEV` across mounts), rename(2)'s replace rules; `RENAME_NOREPLACE`; FAT and `/tmp` |
| 90-94, 260, 268 | `chmod` `fchmod` `chown` `fchown` `lchown` `fchownat` `fchmodat` | stub | W13 | accepted when the file exists, change nothing (FAT and memfs keep no owners or modes) |
| 95 | `umask` | stub | W14 | kept per process (022 at first) and inherited by `fork`; nothing applies it yet |
| 98 | `getrusage` | stub | W14 | `RUSAGE_SELF` `RUSAGE_CHILDREN` `RUSAGE_THREAD`: all zeros, nothing is accounted |
| 99 | `sysinfo` | partial | W14 | uptime, RAM total and free (the nucleus's frame allocator, `nu_mem_info`), the process count; no swap, load average 0 |
| 97, 160, 302 | `getrlimit` `setrlimit` `prlimit64` | partial | W12 | fixed limits (`RLIMIT_NOFILE` 256, `RLIMIT_STACK` the stack size, others unlimited); lowering is accepted, raising past the hard limit `-EPERM` |
| 130 | `rt_sigsuspend` | done | W10 | |
| 143, 145 | `sched_getparam` `sched_getscheduler` | done | W14 | `SCHED_OTHER`, priority 0; `-ESRCH` for no such process or thread |
| 131 | `sigaltstack` | done | W12 | per thread; `SA_ONSTACK` handlers run on it; not inherited by new threads |
| 63 | `uname` | done | W10 | `Burrow burrow 0.10 x86_64` |
| 72 | `fcntl` | partial | W10 | `F_DUPFD(_CLOEXEC) F_GETFD F_SETFD F_GETFL F_SETFL` (`O_APPEND`; `O_NONBLOCK` gives `-EAGAIN` instead of blocking, W11); no locks |
| 79 | `getcwd` | done | W10 |  |
| 80, 81 | `chdir` `fchdir` | done | W10 |  |
| 83, 84 | `mkdir` `rmdir` | done | W10 | writable mounts only; `-ENOTEMPTY` |
| 87 | `unlink` | done | W10 | `-EISDIR` for directories |
| 89, 267 | `readlink` `readlinkat` | done | W14 | there are no symbolic links: `-EINVAL` for a path that exists, else its error |
| 102, 104, 107, 108 | `getuid` `getgid` `geteuid` `getegid` | partial | W9 | everyone is root (0) |
| 109 | `setpgid` | done | W10 | self or a child in the same session |
| 111, 121 | `getpgrp` `getpgid` | done | W10 | |
| 112, 124 | `setsid` `getsid` | done | W10 | |
| 110 | `getppid` | done | W10 | orphans are adopted by init (pid 1) |
| 157 | `prctl` | stub | W10 | `PR_SET_NAME` accepted and ignored, `PR_GET_NAME` = the program's base name; anything else `-EINVAL` |
| 158 | `arch_prctl` | partial | W9 | `ARCH_SET_FS`, `ARCH_GET_FS` (TLS); not GS |
| 186 | `gettid` | done | W12 | the main thread's tid is the pid; tids share the pid number space |
| 200, 234 | `tkill` `tgkill` | done | W12 | a signal for one thread |
| 202 | `futex` | partial | W12 | `WAIT WAKE WAIT_BITSET WAKE_BITSET REQUEUE CMP_REQUEUE`, timeouts (`FUTEX_CLOCK_REALTIME` deadlines since W14), `EINTR`; private to an address space (no PI, no robust lists) |
| 203 | `sched_setaffinity` | done | W14 | one CPU: accepted when the mask holds CPU 0, else `-EINVAL` |
| 204 | `sched_getaffinity` | done | W12 | one CPU |
| 213, 291 | `epoll_create` `epoll_create1` | done | W12 | `EPOLL_CLOEXEC` |
| 217 | `getdents64` | done | W10 | includes `.` and `..`; `d_ino` made up |
| 218 | `set_tid_address` | done | W12 | the word is cleared and futex-woken when the thread exits |
| 228 | `clock_gettime` | done | W9 | since W14 `CLOCK_REALTIME`, `_COARSE`, `_ALARM` and `CLOCK_TAI` are wall time from the CMOS clock (`drivers/rtc`, read once at boot, taken as UTC); every other clock counts from boot; `-EINVAL` for 10 and above 11 |
| 229 | `clock_getres` | done | W14 | 1 ns for every clock |
| 96, 201 | `gettimeofday` `time` | done | W14 | wall time; the timezone `gettimeofday` reports is UTC |
| 164, 227 | `settimeofday` `clock_settime` | done | W14 | `-EPERM`: nobody sets the clock |
| 231 | `exit_group` | done | W9 |  |
| 232, 281 | `epoll_wait` `epoll_pwait` | partial | W12 | level- and edge-triggered, `EPOLLONESHOT`, `EPOLLRDHUP`; the signal mask of `epoll_pwait` is ignored; never restarted |
| 233 | `epoll_ctl` | done | W12 | `ADD MOD DEL` on pipes, the terminal, devices, eventfds; `-EPERM` for regular files |
| 257 | `openat` | done | W10 | `AT_FDCWD` or a directory fd |
| 258, 263 | `mkdirat` `unlinkat` | done | W10 | `AT_REMOVEDIR` |
| 262 | `newfstatat` | done | W10 | `AT_EMPTY_PATH` |
| 269, 439 | `faccessat` `faccessat2` | partial | W10 | existence only |
| 270 | `pselect6` | partial | W14 | as `select` with a timespec; the signal mask is ignored |
| 271 | `ppoll` | partial | W10 | as `poll`; the signal mask argument is ignored |
| 280 | `utimensat` | stub | W13 | accepted when the file exists; times are not set |
| 284, 290 | `eventfd` `eventfd2` | done | W12 | `EFD_SEMAPHORE EFD_NONBLOCK EFD_CLOEXEC` |
| 292 | `dup3` | done | W10 | `O_CLOEXEC` |
| 293 | `pipe2` | done | W10 | `O_CLOEXEC`, `O_NONBLOCK` (`-EAGAIN`; checked again in W14) |
| 318 | `getrandom` | partial | W14 | never blocks; `GRND_NONBLOCK` `GRND_RANDOM` `GRND_INSECURE`; the bytes come from `kernel/random`, a ChaCha8 generator seeded once from RDRAND or the TSC: **not for cryptography** |
| 324 | `membarrier` | stub | W14 | `MEMBARRIER_CMD_QUERY` answers 0 (no commands); others `-EINVAL` |
| 332 | `statx` | done | W14 | the basic fields from the same data as `newfstatat` (`STATX_BASIC_STATS`, no birth time); `AT_EMPTY_PATH` |

**Devices** (W11). `/dev/random` and `/dev/urandom` (W14-S03) read `kernel/random`, the same generator as `getrandom`; writes are accepted and change nothing; neither ever blocks, and neither is for cryptography. `/dev/fb0` is the BGA framebuffer as Linux fbdev (`kernel/fbdev`): 1024x768, 32 bpp XRGB, `line_length` the real pitch; map it with `mmap(MAP_SHARED)`. While any file has it open the text console draws nothing on screen (serial still gets everything) and repaints when the last one closes. `/dev/input/event0` is the PS/2 keyboard as Linux evdev (`kernel/evdev`): `struct input_event` with Linux key codes, press 1, release 0, autorepeat 2, each followed by `EV_SYN`; every open file has its own queue; while any is open the keyboard is grabbed and the console gets no keys. `/dev/input/event1` is the PS/2 mouse (W14-S02): `EV_REL` `REL_X REL_Y REL_WHEEL` (y down, wheel up positive) and `EV_KEY` `BTN_LEFT BTN_RIGHT BTN_MIDDLE`, one `EV_SYN` per packet; it grabs nothing. `/dev/input/mice` is the mouse as Linux mousedev's PS/2 stream: 3-byte packets, 4-byte with the wheel after the IntelliMouse handshake (sample rates 200, 100, 80 written to it), commands answered with `0xFA` (and the id for `0xF2`).

**Terminal.** fds 0-2 of a program are the console terminal (`kernel/tty`): canonical mode with echo, erase (DEL, `^H`), kill (`^U`), end of file (`^D`); `ICRNL`; raw mode through `TCSETS` (a half-typed line becomes input); in raw mode `VMIN` 0 makes a read return at once (`VTIME` 0) or after `VTIME` tenths of a second without input (W13), `VMIN` above 0 waits for a byte. `^C`, `^\` and `^Z` send `SIGINT`, `SIGQUIT` and `SIGTSTP` to the foreground process group (the console's `run` gives it the program's group). Output is not post-processed (the console already turns `\n` into `\r\n`).

**Signals.** Process-directed signals (`kill`) are taken by the first thread, by tid, that does not block them; thread-directed ones (`tgkill`) by that thread; masks and alternate stacks are per thread, actions per process (W12). Delivered when a system call returns, or, for a process that is computing, at its next timer tick (the nucleus stops the thread with an interrupt request). Default actions: terminate (status 128 + n in the shell), ignore for `SIGCHLD SIGCONT SIGURG SIGWINCH`; the job-control stops (`SIGSTOP SIGTSTP SIGTTIN SIGTTOU`) are ignored until stopped processes exist. Blocking calls (pipe and terminal reads and writes, `wait4`) return `EINTR`, or start again with `SA_RESTART`; `poll`, `ppoll`, `pause`, `nanosleep` and `rt_sigsuspend` always return `EINTR`, as on Linux. `SIGCHLD` goes to the parent on every exit. `execve` resets caught signals to the default.

**Faults.** An exception in ring 3 becomes `SIGSEGV`, `SIGILL` or `SIGFPE`: delivered to a handler if the process installed one (with `si_addr` set), otherwise the process ends with the status a Linux shell would show: 139 (SIGSEGV) for page faults and general protection faults, 132 (SIGILL) for invalid opcodes, 136 (SIGFPE) for divide errors. The kernel logs `pid N (prog) killed: page fault at 0x... (rip 0x...)`. **Out of memory** (W14-S01): a first touch of mapped memory when no frame is left (the nucleus keeps 32 MiB for the kernel) kills the process with `SIGKILL` (status 137) and `pid N (prog) killed: out of memory at 0x...`; as on Linux, `mmap` and `brk` reserve without allocating, so they do not fail for lack of RAM.
