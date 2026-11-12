# ADR-0012: A vDSO, and the dispatcher answers what never blocks

- **Status:** Accepted
- **Date:** 2026-11-12
- **Wave:** W25 (S02 the vDSO, S03 the dispatcher, S04 the waits)

## Context
Every system call from ring 3 is a request: the gate queues it in the
nucleus (`nucleus/uthread.c`), the Go kernel's dispatcher takes a batch with
`nu_syscall_wait`, starts a goroutine for each (`go s.handle(r)`, then
`runtime.Gosched`, BUG-0014), and the goroutine answers with
`nu_syscall_reply`. W24-S06 found what that costs: `getpid` took 101-155 us
on QEMU's TCG, a clock read the same (there was no vDSO), and `scp` moved 1
MB/s where TCP moved 5.5 (RISKS.md R-09).

W25-S01 measured each step of one round trip with stamps the nucleus takes
(`nucleus/lattrace.c`, `/dev/lattrace`): for `getpid`, gate to taken 14.5
us, taken to replied 68-70 (the Go side), replied to resumed 16-17. The Go
side was two thirds of the trip, and within it the cost was not the handler
(a few lines) but the hand-off: a goroutine started, the dispatcher
yielding its P, the scheduler's own clock reads trapping too (5.1
`clock_gettime` calls per user `getpid`, S03).

Policy stays in Go (ADR-0003, ADR-0006): the nucleus must not learn what a
file or a process is, and the Linux ABI must not change.

## Options
1. **Move hot calls into the nucleus** (`getpid`, pipes) - fast, but it splits
   one policy over two languages and two locks; a pipe in C is a second
   pipe.
2. **A goroutine per user thread** (started with the thread, fed its
   requests through a channel) - no `go` per request, but each request still
   crosses from the dispatcher to another goroutine and back: the hand-off
   the trace blamed.
3. **Keep the shape, take the hand-off out where it is not needed**: what
   needs no call is not one (a vDSO, as Linux has); calls that cannot block
   are answered on the dispatcher itself, with no goroutine; an answer
   travels with the dispatcher's next wait, one crossing instead of two.
4. **Answer blocking calls that happen not to block on the dispatcher** (a
   pipe write with room) - tried in S04: the writer got faster, but the
   reader the write woke was stranded on the dispatcher's P while the
   dispatcher waited in the nucleus, and the ping-pong got slower.

## Decision
Option 3.

- **The vDSO.** The nucleus keeps a data page (`include/burrow/vdso.h`: a
  sequence count, the TSC at monotonic 0, the multiplier, the realtime base;
  the same arithmetic as `time_monotonic_ns`) and code built from
  `user/vdso/vdso.c` into a small shared object with Linux's names and
  versions (`__vdso_clock_gettime`, `__vdso_gettimeofday`, `__vdso_time`,
  `__vdso_getcpu`, `LINUX_2.6`), position independent with no relocations.
  `kernel/proc/vdso.go` maps `[vvar]` and `[vdso]` into every process and
  names them with `AT_SYSINFO_EHDR`; the nucleus maps the same pages into the
  Go kernel (`go_load.c`), and the gate accepts the vDSO's code as a ring-0
  caller. The system call's monotonic clock is the nucleus's, so both paths
  read one clock.
- **Quick calls** (`kernel/proc/quick.go`): the id getters, `sched_yield`,
  the clock fallbacks and similar calls that cannot block are answered by
  the dispatcher without a goroutine, unless the thread has a signal ready
  (the slow path delivers it). A batch of only quick calls never yields the
  P.
- **Reply and wait** (`nu_syscall_reply_wait`, 0x102D): the dispatcher sends
  its last quick answer with its next wait; the nucleus wakes the answered
  thread and blocks the dispatcher in one crossing.
- **Waits** (`kernel/proc/waitany.go`): `poll`, `select` and `epoll` wait on
  up to four files with a plain `select` statement, `reflect.Select` beyond.

## Consequences
- `getpid` 103 to 52 us, a clock read 110-164 us to 85 ns; the Go kernel
  makes 0.14 system calls per user request instead of 5.1.
- A blocking call still costs a goroutine hand-off (a pipe write: 118 us from
  taken to answered); the ping-pongs did not get faster, and `scp` rose only
  to 1.25 MB/s. What would help next is a dispatcher that hands off its P
  while it waits, so a call answered on it cannot strand the goroutines it
  wakes (R-09).
- Every path that answers a call must keep the slow path's duties: signals
  at return, interrupt stops, CPU time, kills of queued threads (PIT-91).
  The quick path declines whatever it cannot do.
- The vDSO's page layout is a contract between the nucleus and
  `user/vdso`: it lives in `include/burrow/vdso.h` and nowhere else.
- Tests: `user/vdsotest` (the vDSO's clocks against the system call's,
  across CPUs, after `settimeofday`; host and Burrow), `go_stack_sysinfo_ehdr`,
  `TestClocks`, `waitany_test.go`, `make lat-bench`; the quick path has no
  host test of its own and is covered by every smoke (`ports-smoke`'s
  signal and thread programs, `syslat` with `SMP=1`).
