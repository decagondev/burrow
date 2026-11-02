# Go Runtime Syscall Census

**Question:** which Linux system calls does a static Go program make for goroutines, channels, GC and timers? That is the minimum the nucleus shim must implement before `main()` can print.

**Run on:** host Linux, `CGO_ENABLED=0`, `GODEBUG=asyncpreemptoff=1`. First with Go 1.22.2 (sandbox toolchain); **re-run with the pinned Go 1.27.3 in W6-S01** (2026-11-02, Debian WSL2): see "Result (Go 1.27.3)". The pinned set lives in `tests/regression/census-go1.27.txt`.

## Procedure (repeatable)

```sh
cd tools && CGO_ENABLED=0 go build -o /tmp/census ./census   # tools/census/main.go
GODEBUG=asyncpreemptoff=1 strace -f -c ./census          # summary
strace -f -e trace=mmap,clone,arch_prctl,openat,fcntl ./census   # details
```

The test program: one goroutine sends on a channel, ~2000 x 64 KiB allocations with a sliding window, `runtime.GC()`, `time.Sleep(10ms)`, print.

## Result (Go 1.22.2)

| Syscall | Calls | Notes |
|---|---|---|
| rt_sigaction | 114 | installs handlers; we accept and never deliver |
| nanosleep | 88 | `usleep` in spin/backoff loops |
| madvise | 80 | scavenger returns memory. **Not** a no-op: `MADV_DONTNEED` must make pages read as zero, Go 1.27 relies on it (BUG-0009) |
| epoll_pwait | 30 | netpoller = timer sleeping |
| mmap | 27 | see below |
| futex | 18 | all runtime locks / notes |
| rt_sigprocmask | 11 | |
| sigaltstack | 8 | |
| gettid | 7 | |
| fcntl | 6 | `F_GETFD` on fds 0,1,2 at start-up |
| sched_yield | 3 | |
| clone | 3 | `CLONE_VM\|FS\|FILES\|SIGHAND\|THREAD\|SYSVSEM\|SETTLS`, TLS in arg 5 |
| pipe2, epoll_create1, epoll_ctl | 1 each | netpoller init (wake pipe) |
| getrlimit | 1 | `RLIMIT_NOFILE` from `os` init |
| sched_getaffinity | 1 | CPU count |
| openat | 1 | `/sys/kernel/mm/transparent_hugepage/hpage_pmd_size` |
| read, close, write, arch_prctl | 1 each | `ARCH_SET_FS` once for m0 |

22 distinct syscalls (plus `execve`, which is the nucleus's job).

## Result (Go 1.27.3) — the pinned toolchain

23 distinct syscalls (plus `execve`): `arch_prctl clone close epoll_create1 epoll_ctl epoll_pwait eventfd2 fcntl futex gettid madvise mmap nanosleep openat pread64 prlimit64 read rt_sigaction rt_sigprocmask sched_getaffinity sched_yield sigaltstack write`.

Differences from 1.22 that change the shim:

| 1.22 | 1.27 | Shim consequence |
|---|---|---|
| `pipe2` wake pipe | `eventfd2` | the netpoll break object is an eventfd: 8-byte counter reads/writes |
| `getrlimit` | `prlimit64` (read `RLIMIT_NOFILE`, then try to raise it) | answer reads, accept sets |
| `openat` THP size only | also `/proc/self/cgroup`, `/proc/self/mountinfo`, cgroup `cpu.max` (GOMAXPROCS from cgroup limits) | `-ENOENT` for all; `pread64` is only reached when an open succeeds |
| — | `mmap(NULL, 32 MiB, RW)` + `madvise(MADV_NOHUGEPAGE)` at start-up, then ~1.4 MiB and 1 MiB RW maps | without demand paging these cost real frames immediately (~36 MiB of 126 MiB); acceptable at 128 MiB, revisit with a lazy-commit #PF handler if memory gets tight |

Calls: futex 902, nanosleep 309, sched_yield 270, rt_sigaction 114, madvise 60, mmap 44, epoll_pwait 43, rt_sigprocmask 29, sigaltstack 20, gettid 19, clone 9 (same flags as 1.22, TLS in arg 5 / R8).

`-ldflags=-T 0x7e0000000000` still links and runs: segments at `0x7dfffffff000` (R X, ELF headers + text), `0x7e000009b000` (R), `0x7e0000169000` (RW, bss to `0x7e00001a8768`) for `kernel/cmd/kernel` (1.5 MiB ELF).

## Three discoveries that shaped the design

1. **Huge `PROT_NONE` reservations.** At start-up the runtime reserves `128 KiB + 1 MiB + 8 MiB + 64 MiB + 512 MiB + 512 MiB` of `PROT_NONE` address space (arena index and summary structures), then commits small pieces with `MAP_FIXED` + `PROT_READ|PROT_WRITE`. A VM that backs every mapping with RAM dies immediately on a 128 MiB machine. **Mitigation:** reserve/commit split (PIT-05).
2. **Heap at the hint.** `mmap(0xc000000000, 64 MiB, PROT_NONE)` then `MAP_FIXED` commits of 4 MiB there. Our virtual layout keeps `0xC0_0000_0000+` for the Go heap.
3. **Start-up file-descriptor check.** `fcntl(0..2, F_GETFD)`: if a std fd is missing, the runtime tries to open `/dev/null` and throws if that fails. **Mitigation:** fds 0-2 are "the console".

## High text address

`go build -ldflags="-T 0x7e0000000000"` produced segments at `0x7dfffffff000` (headers + text), `0x7e0000081000` (rodata), `0x7e0000124000` (data/bss) and **ran correctly** on Linux. So placing the Go kernel high in the lower half (MEMORY_MAP.md) is viable, pending re-test on 1.27.
