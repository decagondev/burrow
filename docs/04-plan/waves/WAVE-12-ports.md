# W12 Ports  (M12)  — status: in progress

Goal: bringing software to Burrow is routine. Demo: a Go program, a Lua REPL, C++ with exceptions and `std::thread`, and a C compiler running *on* Burrow. Stories: E11 ([BACKLOG.md](../BACKLOG.md)), [ADR-0006](../../03-decisions/ADR-0006-linux-abi-userland.md). The guide written along the way: [PORTING.md](../../06-guides/PORTING.md).

Design (continues W9-W11: the nucleus is mechanism, Go is policy):
- **Threads are Go's business.** The nucleus already runs many ring-3 threads in one address space; what was missing is policy. A `Thread` in `kernel/proc` carries what Linux keeps per thread: its TID (the main thread's is the pid), signal mask and pending set, the EINTR wake-up, TLS (`FS`), alternate signal stack and `clear_child_tid`. The `Process` keeps what threads share: memory, descriptors, signal actions, process-directed pending signals. Handlers that need it (signals, TLS, exit, clone, blocking calls) get the calling thread; the rest keep taking the process.
- **Futexes live in Go.** A wait is a goroutine blocking on a channel, keyed by (address space, address); the value check and the enqueue happen under one lock, as Linux's hash-bucket lock does.
- **epoll is poll that remembers.** An epoll file holds `Poller` items; edge-triggered items fire when the file's change channel has closed since the last report, which is what "edge" means.
- **Ports that need downloads** (Lua, a C++ toolchain, TinyCC) follow the busybox/Doom pattern: pinned, cached outside the tree, skipped when absent, fetched only after a go-ahead.

### [x] W12-S01 Threads (E11)
`clone` with `CLONE_VM|CLONE_THREAD|...` (`SETTLS`, `PARENT_SETTID`, `CHILD_SETTID`, `CHILD_CLEARTID`), `exit` of one thread vs `exit_group`, `gettid`, `set_tid_address`, `futex` (`WAIT`, `WAKE`, `WAIT_BITSET`, `WAKE_BITSET`, `REQUEUE`, `CMP_REQUEUE`, timeouts), per-thread signal masks, `tgkill` to one thread, `sigaltstack` with `SA_ONSTACK`, `sched_yield`, `sched_getaffinity`; execve and fork with threads. `user/threads.c` (musl pthreads: create, join, mutex, condition variable, a thread-directed signal).
As built: `kernel/proc/thread.go` (the `Thread`, a second handler table `ttable` for calls that need the calling thread), `clone.go`, `futex.go`; signal delivery split into `signal.go` and `sigcalls.go`. musl needed `mprotect` for thread stacks. `make ports-smoke` runs `threads`.

### [x] W12-S02 epoll and eventfd (E11)
`epoll_create1`, `epoll_ctl`, `epoll_wait`, `epoll_pwait` over `Poller` files (level- and edge-triggered), `eventfd2`; `madvise`, `prlimit64`.
As built: `epoll.go`, `eventfd.go`, `limits.go`, host-tested; the Go runtime's poller is their on-target test (S03).

### [x] W12-S03 A Go user program (E11)
`user/go/`: a static `GOOS=linux` program built by the host's Go toolchain (goroutines, channels, timers, GC, `os`), run from the console.
As built: `user/go/gohello` (its own module; every `user/go/<name>/main.go` becomes `/bin/<name>`). The runtime's first act, reserving hundreds of MiB for its page allocator, failed until user `mmap`/`brk` memory became demand-zero (`kernel/proc/area.go`): areas, filled at first touch by an answered page fault or a kernel copy. Requests that threads queued just before `exit_group` are now dropped quietly. S01-S03 are one commit: the Go runtime was the test that finished all three.

### [ ] W12-S04 Lua 5.4 (E11) — needs a go-ahead to download its source
### [ ] W12-S05 C++: exceptions and std::thread (E11) — needs a go-ahead to download a musl C++ toolchain
### [ ] W12-S06 A C compiler on Burrow: TinyCC (E11) — needs a go-ahead to download its source
### [~] W12-S07 docs/06-guides/PORTING.md (E11)

Exit: **M12 demo — ports.** `make clean test` green; DEVLOG wave entry.
