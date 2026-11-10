# Bugs and Mitigations

Every bug gets an entry, even five-minute ones: the pattern across entries is the lesson. IDs are never reused. Newest at the top of each section.

Template:
```text
## BUG-NNNN: one-line title
- Found:      date, wave/slice, how (test name, symptom)
- Severity:   blocker | major | minor | latent (found before it bit)
- Symptom:    what you saw (paste the log line)
- Root cause: why it happened
- Fix:        what changed (files)
- Mitigation: what stops the CLASS of bug from returning (test, flag, rule)
- Regression: test id, or why none is possible
- Lesson:     one sentence
```

```mermaid
flowchart LR
    A["Symptom seen"] --> B["Open BUG entry<br/>symptom + log"]
    B --> C["Reproduce reliably"]
    C --> D["Write failing test<br/>REG-NNNN or unit"]
    D --> E["Find root cause"]
    E --> F["Fix"]
    F --> G["Test passes,<br/>make test green"]
    G --> H["Mitigation for the class<br/>flag / rule / doc"]
    H --> I["Close entry, DEVLOG line"]
```

---

## Open

None (BUG-0023 closed in W21-S03).

## Closed

### BUG-0030: Nothing under a mount below a missing directory could be reached
- Found: 2026-11-10, W24-S02: on the ext2 root, `/opt/net/bin/dropbear` was "not found" although the kernel had logged `NETDATA mounted at /opt/net`.
- Severity: major (every data disk mounted under a directory the root lacks was unreachable on an ext2 root: the net disk, and the X and Qt disks with `make run-unix`; the initrd has `/opt`, which is why it hid).
- Symptom: `-sh: /opt/net/bin/dropbear: not found`; `ls /` showed no `opt`.
- Root cause: the VFS listed the mount points directly inside a directory, but a directory that existed only because a mount lay below it (`opt` for `opt/net`) could not be opened or stat'ed, so the walker stopped there; and a listing showed only mounts one level down.
- Fix: `vfs/virtual.go`: `below` gives the entries the mounts under a directory add (a mount point's root, or the directory on the way to a deeper one), and a directory only mounts make opens as a read-only empty one with them; a real directory on the way keeps its own entry. `Open`, listings and `Lstat` use it.
- Mitigation: none needed beyond the test.
- Regression: `TestBUG0030MountBelowMissingDir` (`kernel/vfs`; fails without the fix: the walk stops at `opt`) and `make ssh-smoke`, which runs dropbear from `/opt/net` on the ext2 root.
- Lesson: a test tree that already has every directory hides the case where it does not; the ext2 root is the first tree made from scratch.

### BUG-0029: A process reading its own stat was sometimes "sleeping"
- Found: 2026-11-06, W23-S05, in the full `make test`: `SMOKE FAIL [ports]: timed out after 120s`; proctest had printed `proctest: stat-head FAIL` and the keys file waited for `proctest: ok`.
- Severity: minor (a wrong state letter in `/proc/<pid>/stat` and `status`, and a flaky test; nothing acted on it).
- Symptom: under the suite's load, `/proc/self/stat` said `S` for the process reading it; Linux says `R`.
- Root cause: W23-S01 called a thread sleeping when any system call had held it 10 ms. The snapshot is taken in the reader's own `openat`, which took longer than that on a loaded machine.
- Fix: only the calls that wait (`read`, `poll`, `select`, `nanosleep`, `wait4`, `futex`, `epoll_wait`, `accept`, `recvfrom`, `recvmsg`, `pause`, `rt_sigsuspend`, `waitid`, and their variants) count, and still only after 10 ms (`kernel/proc/procsource.go`, `waits`).
- Mitigation: none needed beyond the test.
- Regression: `TestBUG0029StateIsNotATimer` (`kernel/proc`; fails without the fix: `openat` held long was sleeping) and proctest's `stat-head` in `make ports-smoke`.
- Lesson: a state guessed from a timer is a race with the machine's load; guess from what the thread is doing.

### BUG-0028: system() and posix_spawn crashed the calling program
- Found: 2026-11-05, W22-S03: Doom built with sound stopped in `I_Error` (an unrelated build mistake), whose error box runs `system()`; the program was killed with a page fault in musl's `posix_spawn` child.
- Severity: major (every musl program that calls `system()`, `popen()` or `posix_spawn` died; nothing in the tree had called them, which is why it hid since W10).
- Symptom: `[proc] WARN: pid 3 (doom) killed: page fault at 0x42444c757 (rip 0x443162)`, the rip in `child` (`posix_spawn.lo`).
- Root cause: two. musl's `posix_spawn` calls `clone(CLONE_VM|CLONE_VFORK|SIGCHLD)` onto a stack of its own, where its `__clone` left the function and argument the child pops; `kernel/proc`'s clone took any process-shaped clone as a plain fork and ignored the stack, so the child popped the parent's stack and jumped to garbage. Behind it, `system()` runs `/bin/sh -c LINE`, and Burrow's `user/sh.c` had no `-c`: it read commands from the terminal for ever.
- Fix: the process shape of clone starts the child on the stack it is given (`forkOnto`, `kernel/proc/clone.go`, `process.go`; the copy is still a copy, which `posix_spawn`'s pipe-based exec check does not mind). `sh -c LINE` runs one line without job control (no process groups, never the terminal's foreground) and exits with its status.
- Mitigation: none needed beyond the test; `CLONE_VM` without `CLONE_THREAD` stays a copy, not shared memory.
- Regression: `TestBUG0028VforkCloneStartsOnItsStack` (`kernel/proc`; fails without the fix) and `user/spawntest` in `make ports-smoke` (`system()`, `posix_spawn`, a missing program), which passes on the host's Linux too.
- Lesson: a syscall shape nothing used yet is a bug waiting for the first program that does; the shape is cheap to test when it is added.

### BUG-0023: The console once waited for ever after gohello exited
- Closed: 2026-11-05, W21-S03.
- Found: 2026-11-04, W19-S03, in the full `make test`: `SMOKE FAIL [ports]: timed out after 60s`; the log ends at `gohello: ok` (gohello's last line) with no `burrow>` prompt after it and no kernel message.
- Severity: minor so far (one hang in 18 runs of `make ports-smoke`: the 16 runs since, alone, all passed). Seen again in W19-S04, in a C program: `credtest --exec` stopped after `credtest: kill ok`, in the next case (`peercred`: the parent in `accept`, its child about to `connect`); eight runs since passed. Two programs, one shape: a process waiting for its child, which suggests process exit or the wait for it, not Go's runtime.
- Symptom: a Go user program prints its last line and exits; the console's `run` never returns.
- Root cause (found in W21-S03, by the W20-S01 dump of a `waitstress` hang in a four-CPU `make test`: the parent 70 s in `wait4`, its child "exiting" with no threads, and one goroutine in `exit`'s `Nucleus.Kill(821)`): two kills of one thread at once. `System.handle` killed a thread whose request came in after its process had started to end (`t == nil && late`), but whoever marked the thread gone (`exit`, `execve`'s `endOthers`, the thread's own `exit`) always kills it too. In the nucleus both `uthread_kill`s found the thread and both called `thread_join`; the first reaped it and freed the slot, and the second waited for a T_DEAD that a free slot never shows, holding `exit` (and so the parent's `wait4`, or the console's `run`) for ever. With one P the two handlers rarely ran at the same moment, which is why it was once in tens of runs; four Ps (W20-S08) made it reproducible.
- Fix: `System.handle` drops a late request without killing (`kernel/proc/system.go`). In the nucleus a second concurrent `uthread_kill` only waits for the first one's join and does not drop the address space's thread count again; `thread_join_id` joins the thread id the caller found, lets one joiner reap it (T_DEAD -> T_REAPING -> T_FREE) and returns when the slot is free or someone else's (`nucleus/thread.c`, `uthread.c`).
- Regression: `TestBUG0023LateRequestDoesNotKillAgain` (`kernel/proc`: a request from a thread after exit_group must not kill it a second time; fails without the fix) and ktest `bug0023_join_twice` (sched suite, one and four CPUs: a second join of a reaped thread returns, two threads joining one target both return and free its stack once).
- W20-S01's hunt: a hang watchdog now makes every timed-out smoke end with a dump of every process, thread and kernel goroutine (`tests/smoke/sendkeys.py` sends Alt+SysRq; `proc.System.Dump`); `user/waitstress` (children exiting at once, with threads running, from a thread, killed while asleep, leaving orphans, exec'ing) ran about 5,000 rounds and `gohello` 255 times over 17 boots, three at a time on a loaded host, without a hang. It found BUG-0026 instead (handlers leaked by threads that end while blocked), which is not this one's cause: the nucleus never reuses thread ids, so a stale reply only fails. Still open; the next sighting comes with its dump.
- Lesson: a cleanup with two owners runs twice; and "rare" was a one-CPU timing that a second CPU turned into a test failure. The dump set up in W20-S01 caught it on its first sighting.

### BUG-0027: Two CPUs faulting on one page: the second was called a bad pointer
- Found: 2026-11-05, W20-S08, the first go-smoke with the Go kernel on four Ps: `[trap] fault in the Go kernel (#PF, address 00007d0046e2e008)` in `runtime.(*spanSet).push`, a write to a page the runtime had just committed.
- Severity: critical (a panic of the whole kernel from ordinary memory use, on every boot with GOMAXPROCS>1; the same race in user processes would send SIGSEGV to a correct multi-threaded program).
- Symptom: a not-present #PF on demand-zero memory reported as a nil or bad pointer; on one CPU never seen.
- Root cause: demand-zero pages (BUG-0012) are filled by the first fault. Two CPUs touching the same unfilled page both fault; the first fills it under `g_vm_lock`, and the second, once it has the lock, no longer finds the "discarded" marker, so `vm_fault_in` answered "a real fault". `kernel/proc`'s `Mem.FaultIn` had the same shape for user pages: a not-present read on a page another thread had just filled, or a write on a copy-on-write page another thread had just broken, was taken for a protection fault. One CPU could not interleave them: the first fault always finished before the second began.
- Fix: `vm_fault_in` returns "resume" when the page is present by the time it looks (`nucleus/vmm.c`); `FaultIn` resumes whenever the page's protection now allows the access (`kernel/proc/area.go`). A retried access that is still refused comes back as a present fault and is reported then, so nothing loops.
- Mitigation: every fault handler answers "what does the page allow now?", never "what did I expect to find?".
- Regression: ktest `bug0027_fault_in_present` (mm suite: a present page answers 1, an absent unmarked one 0) and `TestBUG0027SecondFaultOnAPageAlreadyHandledResumes` (`kernel/proc`: a second write fault after the copy, and a not-present read of a backed page, resume); the Go test fails without the fix.
- Lesson: "interrupts off" was never the reason faults were safe; one CPU was.

### BUG-0026: A thread that ended while blocked left its handler waiting for ever
- Found: 2026-11-05, W20-S01, in the first hang dump (`waitstress` in ports-smoke, timed out for an unrelated reason): 30 goroutines in `sysPauseFn` for threads of processes long gone.
- Severity: major (a leak without bound: every thread blocked in a system call when its process ends keeps a goroutine, its Thread and its Process for the life of the kernel).
- Symptom: `[dump]   burrow/kernel/proc.sysPauseFn(...)` thirty times, for thread ids far below the live ones.
- Root cause: blocking calls wait on the thread's `sigWake` channel (`t.cancel()`), which only a deliverable signal closes. `exit` (and `execve` ending the other threads) marked the threads exited and killed them in the nucleus but never closed their channels, so a thread in `pause`, a blocking read, `poll` or a futex wait, other than the one ending the process, waited for ever. A signal aimed at the dying thread itself did close it, which is why the obvious test passed.
- Fix: `exit` and `execve`'s `endOthers` close each ended thread's wake channel; `finish` sends no reply for a thread that has ended (`kernel/proc/process.go`, `exec.go`, `signal.go`).
- Mitigation: the hang dump lists every handler goroutine, so a leak shows up in any dump; waitstress in `make test`.
- Regression: `TestBUG0026AKilledProcessLeavesNoHandlers` (`kernel/proc`): one thread paused, another calls `exit_group`; no pause handler may survive. On Burrow, 100 waitstress rounds then a dump show none left (30 before).
- Lesson: a test that kills the blocked thread itself tests the easy case; the leak was in the threads nobody signalled.

### BUG-0025: fstat of a file open for writing said 0644 and root
- Found: 2026-11-04, W19-S06, the first run of the M19 demo: after `passwd alice`, `/etc/shadow` was mode 0644 (it had been 0600), readable by everyone.
- Severity: major (a password file left readable; any program that copies a file's mode or owner from `fstat` copied the wrong ones).
- Symptom: `debugfs -R "stat /etc/shadow"` after the demo: `Mode: 0644`.
- Root cause: a file opened for writing is a `wfile` (its contents buffered, written back whole), and `wfile.Stat` made up its answer: mode 0644, no owner, the time now. busybox's `update_passwd` opens the old file read-write, `fstat`s it and gives the new file (`/etc/shadow+`, then renamed over the old) that mode and owner. Harmless before W19-S04, when nothing had owners.
- Fix: `wfile.Stat` asks the file system for the file and keeps only the size of what was written (`kernel/proc/wfile.go`).
- Mitigation: the demo checks the accounts files after two boots; the regression test reads owner and mode through `fstat`.
- Regression: `TestBUG0025FstatOfAFileOpenForWriting` (`kernel/proc`).
- Lesson: every made-up answer is a bug waiting for the feature that reads it: S04 gave files owners, and the one place that invented them stayed.

### BUG-0024: Every program on the initrd was packed without an execute bit
- Found: 2026-11-04, W19-S04, the first full `make test` with execute permission checked: boot_to_go's shell said `sh: ls: Permission denied` for every program (the console's `run`, which uses `Spawn` and checks nothing, still worked).
- Severity: latent since W10 (nothing checked modes until S04), then a blocker.
- Symptom: `tar tvf build/initrd.tar` lists `/bin/*` as `-rw-r--r--`.
- Root cause: the initrd was packed with `tar --mode=a-rwx,u+rwX,go+rX`, meant to normalise modes taken from a build tree on WSL's `/mnt/c` (which keeps none: everything looks 0777). `X` adds execute only to directories and files that already have it, and `a-rwx` had just taken it away. The FAT disks had the same problem in another form: FAT reported files 0444.
- Fix: `scripts/mkinitrd.py` packs every initrd (reproducibly: sorted names, owner root, time 0) with modes decided by content: directories, ELF files and `#!` scripts 0755, the rest 0644, with `PATH=MODE[:UID:GID]` overrides for S06's `/etc/shadow` and set-uid `su`. FAT reports what Linux's vfat shows with `umask=022`: 0755.
- Mitigation: modes are now part of the image's contents: a smoke test that runs a program through `execve` (boot_to_go's shell, busybox, ports) fails at once if they are wrong.
- Regression: boot_to_go's shell session; `TestOwnersAndModes` (tarfs) for the headers' modes reaching the kernel.
- Lesson: a field nobody reads is untested, however long it has been written: the day a check starts reading it, every old value is suspect.

### BUG-0022: A blocked epoll_wait never saw items added after it began
- Found: 2026-11-04, W18-S06, in the full `make test`: net-smoke failed about one run in six with `gofetch: Get "https://host:PORT/hello.txt": dial tcp 10.0.2.2:PORT: i/o timeout`, and the TLS server logged nothing for that connection.
- Severity: major (any Go program that dials or accepts while its netpoller sleeps can stall for as long as the poller's next timer).
- Symptom: a pcap of a failing run (QEMU `filter-dump`) showed the handshake done in 2 ms (SYN, SYN-ACK, ACK) and then nothing from Burrow until its FIN exactly 30 s later: `http.DefaultTransport`'s dial timeout.
- Root cause: `epollWait` selected only on the change channels of the items present when it scanned. Go's netpoller is usually parked in `epoll_wait` with a timeout while another thread runs `connect` → EINPROGRESS → `epoll_ctl(ADD)`; the SYN-ACK then closed a channel no wait was watching, and the dial waited for the poller's timer. Linux wakes waiters when an added item is ready. It passed most runs because other activity (a timer, the netpoll break eventfd) happened to wake the poller first.
- Fix: the epoll has its own change channel, closed by ADD and MOD and waited on by every blocked wait (`kernel/proc/epoll.go`).
- Mitigation: a waiter that selects over a set must also wait for the set to change; noted in the file's header comment.
- Regression: `TestBUG0022EpollAddWakesBlockedWait` (`kernel/proc`): a wait blocks on an idle eventfd, another thread ADDs a readable one, the wait must return it at once (it timed out after 2 s before the fix); net-smoke itself.
- Lesson: when a flake's failing side logs nothing, capture the wire: it separated "the packet never came" from "nobody was told", in one run.

### BUG-0021: The gc_stress selftest's heap limit sat 12 MiB above a normal run
- Found: 2026-11-04, W17-S01, in the full `make test`: x11-smoke's boot stopped at `[kernel] INFO: selftest FAILED: gc_stress: heap grew to 79 MiB (limit 64 MiB)`; the same image passed twice right after, at 52 MiB.
- Severity: minor (a flaky test, not a kernel fault: the check runs before any driver, and the GC did free, the heap only peaked higher once).
- Symptom: once in a long while, a Go image fails its boot selftest, on any image, independent of the change being tested.
- Root cause: the check churns 1280 MiB through a 16 MiB live window and fails when `HeapSys` passes 64 MiB. With `GOGC=100` a normal run takes 47-55 MiB of heap and 52 from the OS; on Go's single P (BUG-0014) the background mark worker can be late for a cycle when the host is busy, the allocating goroutine runs ahead, and the heap grows before assists catch up. The limit was set from the runs of W6, without room for that.
- Fix: the limit is 128 MiB (`kernel/selftest/selftest.go`): ten times below what is allocated, so a GC that does not free still fails at once, and more than twice a normal peak, so a late cycle does not.
- Regression: `TestGCStressLimitLeavesRoom` (`kernel/selftest`) checks that the kernel's limit is at least twice the live window's normal heap and far below the bytes allocated; every Go boot runs the check itself.
- Lesson: a threshold set from a handful of runs on an idle machine needs a margin argued from the mechanism, not from the numbers seen.

### BUG-0020: Key codes an X server never read reached the shell after it
- Found: 2026-11-03, W16-S03, in the full `make test`: after st and Xfbdev exited, the console's shell read `exit` with a stray byte 0x9C in front (the release of Enter in `K_MEDIUMRAW`) and stayed.
- Severity: minor (since W14-S08: whatever a program in `K_RAW` or `K_MEDIUMRAW` left unread became the console's input).
- Symptom: a stray byte at the start of the first line typed after an X session; it showed only when the session ended on a key (the Enter of `exit` in st), whose release came after X had stopped reading.
- Root cause: Linux's `KDSKBMODE` drops the terminal's unread input when the keyboard mode changes (`tty_ldisc_flush` in `vt_ioctl`); Burrow kept it, and kept it too when an exiting process's mode was undone.
- Fix: `vt.VT` calls a `Keyboard` hook when the mode changes, by `KDSKBMODE` or at exit; `proc.SetVT` hooks it to the console terminal's new `Flush` (`kernel/vt/vt.go`, `kernel/proc/vtcalls.go`, `kernel/tty/control.go`).
- Regression: `TestKbModeChangeFlushesInput` (`kernel/proc`); `make x11-smoke` types `exit` on the console after st's session.
- Lesson: a mode switch is also a boundary for buffered data; check what the real kernel does with the bytes in flight, not only with the mode.

### BUG-0019: ioctl requests with the top bit set never matched
- Found: 2026-11-03, W16-S03: musl's `openpty` failed with `EINVAL` on Burrow, while the same `ptytest` binary passed on Linux.
- Severity: major, latent since W10: every `_IOR` request (`TIOCGPTN`, `EVIOCGVERSION`, `FBIOGET_*` are `_IOR` or numbered with bit 31 set) reached the kernel as a 64-bit value with the upper half all ones.
- Symptom: `ioctl(m, TIOCGPTN, &n)` answered `-EINVAL`; evdev's version and id requests were answered only through a fallback that masks the request, which is why Doom and Qt never showed it.
- Root cause: musl declares `int ioctl(int, int, ...)`, and passing the `int` to the system call sign-extends it; Linux's `ioctl` takes an `unsigned int` and so reads only the low 32 bits. Burrow compared the whole register.
- Fix: `sysIoctlFn` keeps the low 32 bits of the request before any dispatch (`kernel/proc/ttycalls.go`).
- Regression: `TestIoctlRequestIs32Bits` (`kernel/proc`); `ptytest` in `make ports-smoke`.
- Lesson: an argument's C type is part of the ABI; read each system call's arguments at the width Linux declares, not the register's.

### BUG-0018: /dev/input/mice stalled and misaligned TinyX's mouse handshake
- Found: 2026-11-03, W16-S02: under `Xfbdev`, a click never reached `xhello`; with the write fixed, a left click arrived as buttons 3 and 5.
- Severity: major for any program that talks PS/2 to `/dev/input/mice` (since W14-S02; the W14 tests wrote the handshake in one call and read it all back, so they passed).
- Symptom: TinyX wrote the first byte of the IntelliMouse knock (`0xF3`) and nothing more; then, after the first fix, it chose the 3-byte protocol while the device sent 4-byte packets.
- Root cause: two differences from Linux's mousedev. `Poll` reported POLLIN *instead of* POLLOUT while bytes waited, so the next nonblocking `write` of a command got `EAGAIN` and `select` never said writable; and every command byte queued its own ACK behind the motion, where mousedev keeps one pending answer, replaced by each byte written and read before any motion. TinyX writes seven bytes and then expects `FA 03`; seven ACKs ahead of the id made `ps2SkipInit` miss it, so it fell back to plain PS/2.
- Fix: the device is always writable, and a command's answer replaces the pending one and is read first (`kernel/evdev/mice.go`).
- Regression: `TestMiceAlwaysWritable` and the handshake case of `TestMice` (`kernel/evdev`); `make x11-smoke` clicks in `xhello`.
- Lesson: an emulated device must match the real one's timing-free behaviour exactly; a test that writes everything at once and reads everything back cannot see how a driver that waits for each answer will get on.

### BUG-0017: Reading a disk cost about 340 microseconds a sector
- Found: 2026-11-03, W15-S05: `qtbench` took 11 s from `execve` to `main`; a timed `execve` spent 10.5 s of it reading the 15 MB program from the FAT disk.
- Severity: major (since W8: every read from a data disk, Doom's WAD included, ran at about 1.4 MB/s under QEMU).
- Symptom: start-up time grew with the size of the file read, not with anything the program did; faster port I/O changed nothing.
- Root cause: two layers each asked for the smallest unit. FAT read one cluster per call (one sector, on a 64 MiB volume), so every 512 bytes was its own ATA command; and READ SECTORS hands over one sector per data request, which QEMU fetches from the image on its own each time. Moving the words cost too, but less: the `rep insw` tried first changed nothing only because the driver's port wrapper hid it (found in W15-S06); once it really ran, it saved another second.
- Fix: FAT's `ReadAt` reads a run of clusters that lie in a row with one call (0.4 s saved); the ATA driver turns on multiple mode at IDENTIFY and reads with READ MULTIPLE, 16 sectors per data request, each block with one `rep insw` (`drivers/ata/multiple.go`; 10.5 s down to 2.1 s). `execve` to the first Qt frame went from 11.6 s to 2.8 s, and to 1.8 s once W15-S06 let the string I/O through the driver's port wrapper (`drivers/ata/probe.go` keeps `hal.PortIOString`).
- Mitigation: `make qt-bench` fails above 3.5 s to the first frame (`tests/smoke/qt-budgets.py`).
- Regression: `TestContiguousClustersReadAtOnce` (`kernel/fs/fat`), `TestIdentifyAndConformance` checks READ MULTIPLE (`kernel/drivers/ata`).
- Lesson: time each layer before optimising one; the obvious suspect (port I/O) was innocent, and the cost was per request, twice over.

### BUG-0016: QProcess read a child's output one byte at a time
- Found: 2026-11-03, W15-S02: `qthello`'s QProcess check got "h" instead of "hello from a child" on Burrow; on Linux the same binary passed.
- Severity: minor (since W10: any program that sized its reads with `FIONREAD` on a pipe got `-ENOTTY`).
- Symptom: `QProcess::readAllStandardOutput` returned the first byte only; nothing was logged, as `ioctl` itself is implemented.
- Root cause: `ioctl(FIONREAD)` was answered for the terminal and sockets but not pipes, which fell through to `-ENOTTY`. Qt's QProcess reads as many bytes as `FIONREAD` reports and, when it fails, one.
- Fix: pipe ends implement `Ioctler` with `FIONREAD` (the bytes buffered).
- Mitigation: none general: the Qt census counts system calls, not ioctl requests, so it could not see this; `qthello` now prints what QProcess got when its check fails.
- Regression: `TestBUG0016PipeFIONREAD` in `kernel/proc/fd_test.go`; `qthello: process ok` in `tests/smoke/expect/qt.txt`.
- Lesson: a syscall census sees `ioctl` as one call; the requests behind it are their own interface and need their own list.

### BUG-0015: A write to a pipe with no reader never raised SIGPIPE
- Found: 2026-11-03, W14-S06: planning sockets "with EPIPE and SIGPIPE like pipes" showed that pipes had only the EPIPE half.
- Severity: minor (since W10: a writer got `-EPIPE` and had to notice; `yes | head` style pipelines left the writer running until it checked its errors).
- Symptom: `write` to a pipe whose read end was closed returned `-EPIPE` and the writer carried on; Linux also sends `SIGPIPE`, whose default action ends it.
- Root cause: `Thread.write` passed the pipe's `EPIPE` straight back; nothing raised the signal, and no test wrote to a pipe without ignoring the result.
- Fix: `Thread.write` sends `SIGPIPE` to the calling thread whenever a write ends in `EPIPE` (pipes and stream sockets alike); `send`, `sendto` and `sendmsg` do the same unless `MSG_NOSIGNAL`.
- Mitigation: `socktest`'s `sigpipe` check runs on the host's Linux and on Burrow in `make ports-smoke`, so the two must agree.
- Regression: `TestBUG0015PipeSIGPIPE` in `kernel/proc/fd_test.go` (status 141).
- Lesson: an error code is half of some Linux contracts; check the signal that goes with it (`SIGPIPE`, `SIGCHLD`, `SIGTTOU`).

### BUG-0014: Every system call and page fault from a process took about 2 ms
- Found: 2026-11-03, W14-S01: `memtest` needed 8 s for 16 MiB; a probe timed 2000 `getpid` calls at 4 s.
- Severity: major (since W9: every user program ran at up to 500 system calls a second; Doom hid it with few calls per frame).
- Symptom: 2000 us per `getpid` and per first-touch page fault, the same at 128 MiB and 2 GiB.
- Root cause: the dispatcher (`System.Serve`) waits for requests with a blocking `syscall.Syscall`, which keeps its M's P in `_Psyscall`. With one P, the handler goroutine it had just started sat in that P's run queue until sysmon took the P back, and sysmon sleeps in steps of the nucleus's 1 ms tick: about two ticks per request.
- Fix: `Serve` calls `runtime.Gosched()` after starting the handlers, so they run (and reply) before the dispatcher blocks again. 84 us per call, 100 us per fault (QEMU TCG).
- Mitigation: `user/syslat.c` in `make mem-smoke` fails above 500 us per call or fault. Any goroutine that blocks in the nucleus while others have work holds the only P until sysmon notices; IRQ waits and timers still pay that (milliseconds, fine for input), and a tickless nucleus timer would remove it for good.
- Regression: `syslat: ok` in `tests/smoke/expect/mem.txt`.
- Lesson: with GOMAXPROCS 1 a blocking syscall stops the world until sysmon wakes, and sysmon is only as quick as the kernel's sleep; measure the per-call cost before blaming the work.

### BUG-0013: The Doom smoke test dumped the screen during Doom's screen melt
- Found: 2026-11-03, W11-S05: `make doom-smoke` failed 2 runs in the first 3 and was put down to a one-off; seen again in W13-S03 (2 failures in 3).
- Severity: minor (the test, not the kernel: Doom ran correctly every time).
- Symptom: `doom.screen` pixels on the title or the status bar were darker or wrong (`#141414` for `#1c1c1c`, `#740101` for `#8b8b8b`); the dumped picture showed the title or E1M1 half drawn in columns.
- Root cause: Doom's wipe ("melt") drops the new screen in columns at random speeds. `doom.keys` dumped the screen after a fixed `sleep 4`; when the level's first frames were slow (QEMU TCG, a busy host), the melt was not finished.
- Fix: `sendkeys.py` gains `waitpixel <x> <y> <#rrggbb>`: dump the screen until that pixel shows the colour. `doom.keys` waits for the pixels nearest the bottom of each screen, the last ones the melt reveals, before the real dump.
- Regression: `make doom-smoke` 6/6 after the fix (3/6 before it, with a one-pixel wait).
- Lesson: a guest that shows progress only on screen needs a screen condition to wait for, not a time; and two unexplained failures out of three are a bug, not bad luck.

### BUG-0012: Eager commit let the Go runtime's sparse mappings exhaust RAM (GC stress OOM)
- Found: 2026-11-02, W9-S05: `make go-smoke` failed about 1 boot in 3 with `fatal error: runtime: out of memory` in the GC stress, before any user program ran.
- Severity: major (the Go kernel dies; it had been one heap-size wobble from failing since W6).
- Symptom: the new diagnostic said `[vm] ENOMEM: no free frame (26 regions, 0 frames free, 31575 pages mapped below the kernel half)`, while the runtime reported only 47-51 MiB obtained from the OS.
- Root cause: `mmap` backed every committed page at once. The Go runtime maps large read-write areas it touches only sparsely (arena metadata sized for the whole address space) and relies on Linux backing pages on first touch; here those areas cost real frames, ~75 MiB of a 123 MiB machine. And `madvise(MADV_DONTNEED)` zeroed pages but never gave frames back (BUG-0009's fix), so the footprint only grew. W9 did not cause it; a slightly larger kernel and heap moved the boot past the edge. PIT-05 had only covered `PROT_NONE`.
- How it was found: a once-per-kind ENOMEM log in `vm_region.c` (free frames, regions) proved it was frames, not the region table; counting present lower-half pages showed Go's half held all of RAM; `runtime.MemStats` (now in the gc_stress line) showed Go believed it used a third of that.
- Fix: demand-zero pages. A software bit on a not-present leaf (`PTE_DISCARDED`, `common/paging.c`) means "zero-fill on demand": committing read-write memory writes that marker (`pt_mark_lazy`) instead of taking a frame, `MADV_DONTNEED` frees the frame and leaves the marker (`pt_discard`), and a ring-0 #PF on a marked page maps a fresh zeroed frame (`vm_fault_in`). `PROT_NONE` and release clear markers so those pages still fault. Running out of frames in a fault now panics with "out of memory" rather than a #PF report.
- Regression: unit tests `paging_discard_marks_and_remaps`, `paging_mark_lazy`; ktests `vm_reserve_commit` (commit costs tables only, the first touch one frame) and `madvise_returns_frames`; `make go-smoke` 10/10 including a run whose heap peaked at 59 MiB.
- Lesson: "commit" in Linux means "you may touch this", not "this is backed"; programs (and runtimes) build on that.

### BUG-0011: mkfat images had no volume-label entry in the root, so fsck.fat rejected them
- Found: 2026-11-02, the first CI run of Wave 8 (`make fat-interop` on ubuntu-latest).
- Severity: minor (QEMU and our reader were happy; Linux's checker was not).
- Symptom: `FAT INTEROP FAIL: fsck.fat rejects build/data.img`; locally `fsck.fat -n` said "Label in boot sector is 'BURROWDATA', but there is no volume label in root directory" and exited non-zero.
- Root cause: `fat.Format` wrote the label only in the boot sector. FAT tools expect the root directory's first entry to repeat it (attribute `ATTR_VOLUME_ID`).
- How it was found: the interop check had only run in CI (no dosfstools locally). Reproduced without root by unpacking the Debian packages (`apt-get download dosfstools mtools`, `dpkg -x`) and putting them on PATH.
- Fix: the root directory starts with a volume-label entry (`kernel/fs/fat/format.go`); the reader already skipped such entries. The interop script now prints fsck.fat's output when it fails.
- Regression: `TestRootRepeatsTheLabel` in `kernel/fs/fat`; `make fat-interop`.
- Lesson: a format writer is only as good as the strictest reader: test against the reference tools early, not first in CI.

### BUG-0010: The census parsed strace's summary table, whose layout differs between versions
- Found: 2026-11-02, the first CI run of Wave 6 (`make census` on ubuntu-latest).
- Severity: minor (CI failed; the shim was fine).
- Symptom: `CENSUS FAIL ... new syscalls: ...>` on CI, while the same commit passed locally (Debian, strace 6.13).
- Root cause: `census.sh` took the last column of each `strace -c` row as the syscall name. The runner's strace printed a row whose last field was `...>`.
- Fix: run `strace -f -qq` and take the name from each per-call line (`PID name(`). That also counts `exit_group`, which `-c` omits because it never returns, so it joins `census-go1.27.txt` (24 syscalls; the shim already had its row).
- Regression: `make census` in CI on ubuntu-latest.
- Lesson: parse a tool's stable output, not its human-formatted summary.

### BUG-0009: A no-op madvise handed Go stale memory it believed was zeroed
- Found: 2026-11-02, W6-S11, the new GC-stress selftest: about 1 boot in 8 faulted writing into Go's rodata.
- Severity: major (silent heap corruption in the Go kernel).
- Symptom: `[trap] #PF at rip=...memmove err=3 cr2=7e00000af0c2` while formatting a string after the GC stress. A fresh `fmt.pp` already contained `{ptr "adx", 3, &X86.HasADX}`, old bytes from `internal/cpu`, so its buffer pointed into string data. Identical address in every failure.
- Root cause: Go 1.27 `mheap.initSpan` skips clearing fully scavenged spans because "the kernel guarantees they will be zero-filled on next use" after `MADV_DONTNEED` (default `madvdontneed=1`). The shim treated `madvise` as advice (no-op, as the 1.22 census suggested), so scavenged pages kept their old contents.
- How it was found: the trap report now prints memory behind RDX/RSI/RSP (`peek` in `trap.c`); the frame allocator got an allocated-bitmap that panics on double free or double hand-out, which ruled out physical aliasing; that left the one place the nucleus deliberately kept contents.
- Fix: `madvise(MADV_DONTNEED)` zeroes the range and keeps it mapped (`vm_discard`, `nucleus/shim_mm.c`); other advice stays a hint.
- Mitigation: the census and hosting docs now say which advice has semantics. The frame bitmap and the register peeks stay in.
- Regression: ktest `madvise_dontneed_zeroes` (fails with the old no-op); the GC stress in `make go-smoke` (0 failures in 50 boots after, 4-6 in 40 before).
- Lesson: "advice" in Linux man pages can be a guarantee that a runtime builds on; read the consumer (`mheap.go`), not only the syscall's name.

### BUG-0008: VM region operations were check-then-act without a lock
- Found: 2026-11-02, W6-S11, while hunting BUG-0009 (no failure was traced to it).
- Severity: latent major: the Go runtime's Ms call mmap/munmap concurrently and each syscall runs with interrupts on.
- Symptom: none observed. Path: `vm_commit` asked `regions_covers` and then mapped page by page with no lock, while another thread's `vm_reserve` could shift the region array; a wrong "covered" could map pages outside any reservation, which a later reservation would then reuse with old contents.
- Fix: every `vm_region.c` operation holds a per-address-space sleeping mutex (`nucleus/mutex.h`, futex-based); `mmap` is one atomic `vm_map_anon`; a commit into a fresh reservation panics if it finds a page already mapped.
- Regression: none possible to trigger reliably; the "already mapped in a fresh reservation" panic is the tripwire.
- Lesson: on a preemptive kernel every multi-step operation on shared state needs a lock, even on one CPU.

### BUG-0007: A thread woken before it switched out kept running as T_RUNNABLE
- Found: 2026-11-02, W5-S11, reading `schedule()` while designing `futex_wait` (before any test hit it).
- Severity: latent (would have let pick_next hand the running thread to itself again and confused join/wake logic).
- Symptom: none yet. Path: thread marks itself T_BLOCKED, a waker (or the tick) sets it T_RUNNABLE before `schedule()` runs, `pick_next` returns the thread itself, `schedule()` returned early without restoring T_RUNNING.
- Root cause: the `next == prev` early return assumed prev was still T_RUNNING.
- Fix: `nucleus/sched.c` sets `prev->state = T_RUNNING` on that path.
- Mitigation: blocking primitives go through `thread_block_unlock`, which marks the thread blocked before the lock drops, so wake-before-switch is a designed-in case, not an accident (PIT-23).
- Regression: ktest `futex_ping_pong` (20,000 wake/wait pairs) exercises the path continuously.
- Lesson: every early return in a state machine must leave the state consistent.

### BUG-0006: CR0.WP was off, so ring 0 ignored read-only pages (W4's W^X was not enforced)
- Found: 2026-11-02, W5-S07, writing the `vmm_kernel_wx` ktest.
- Severity: latent major: the loader mapped nucleus text read-only, but with CR0.WP=0 the CPU lets supervisor code write to any present page.
- Symptom: none yet; would have shown as a stray kernel write silently patching nucleus code.
- Root cause: neither stage2 nor the loader sets CR0.WP; it is off after reset and only matters for ring 0.
- Fix: `vmm_init` sets CR0.WP (`nucleus/vmm.c`).
- Mitigation: ktests that try the forbidden access (`vmm_kernel_wx`, `vmm_protect_readonly`) instead of only reading back the flags; a table walk proved the flags were right all along.
- Regression: ktest `vmm_kernel_wx` (write to nucleus text must #PF with P=1).
- Lesson: check that a protection is enforced, not only that it is configured.

### BUG-0005: A regression case whose mutator failed was silently skipped
- Found: 2026-11-02, W4-S03, while adding REG-0004 (its `lba_of` helper failed on its first version).
- Severity: major (test harness): a broken case vanished from the count instead of failing.
- Symptom: `make regress` printed `3 passed, 0 failed` with no line at all for REG-0004.
- Root cause: `case_run` ran the mutator as a bare command; its failure aborted the case without touching `pass`/`failn`, and nothing compared the number of cases run with the number declared.
- Fix: `tests/regression/run_all.sh` treats a failing mutator as `FAIL <name> (mutator ... failed)`; mutators that compute offsets return non-zero on error.
- Mitigation: rule: a test harness must report every case it was asked to run. Read the per-case lines, not only the totals.
- Regression: none automated (a deliberately broken mutator would make `make regress` red); the REG-0004 line itself is the visible check.
- Lesson: test the tester; a total without the per-case lines hides missing cases.

### BUG-0004: `KEY_ENTER` defined by both `<linux/input.h>` and `doomkeys.h`
- Found: 2026-11-02, while compile-checking `snippets/userland/doomgeneric_burrow.c` (`-Werror`).
- Severity: latent (would have shipped as "Enter key does nothing / acts as '='").
- Symptom: macro redefinition warning; `case KEY_ENTER` would compare the evdev code against Doom's value 13 (`KEY_EQUAL` in evdev).
- Root cause: two headers from different worlds share a name with different meanings (evdev 28, Doom 13).
- Fix: capture `EVDEV_KEY_ENTER = KEY_ENTER` before including `doomkeys.h`, `#undef`, map explicitly.
- Mitigation: build ported code with `-Werror`; when bridging two APIs, prefix constants from one side.
- Regression: `tests/unit/doom/keymap_test.c` (W11), run by `make doom`: evdev Enter maps to Doom's Enter, evdev 13 (`=`) does not.
- Lesson: compile-check reference code, don't just read it.

### BUG-0003: A corrupted loader failed for the wrong reason (cannot self-verify)
- Found: 2026-11-02, W3, regression case REG-0001 (flip one byte in the loader image).
- Severity: major (design flaw).
- Symptom: expected `PANIC: loader CRC mismatch`; got `PANIC: need at least 32 MiB of RAM`. The flipped byte had changed code/data used *before* the self-check.
- Root cause: a program that might be corrupted cannot be relied on to detect its own corruption; the check ran inside the thing being checked.
- Fix: stage2 computes CRC-32 of the loader (32-bit asm `crc32_ieee`) and compares with the manifest **before** jumping (`[stage2] FAIL K`). Loader self-check kept as defence in depth.
- Mitigation: rule "verify before you execute" for every image the loader/nucleus loads (W4-S03, W6-S02). All failure paths also write debug-exit so tests fail in milliseconds.
- Regression: REG-0001-loader-crc.
- Lesson: negative tests exercise the code that matters most when things go wrong.

### BUG-0002: ELF unit test fixture smaller than the segment it described
- Found: 2026-11-02, W3-S10, `elf_load_segment` failed under `make test-c`.
- Severity: minor (test bug, parser correct).
- Symptom: `s.memsz == 0x0, want 0x2000`.
- Root cause: segment data at file offset 0x1000 in a 4096-byte buffer, so `offset + filesz > len`; the parser correctly rejected it.
- Fix: buffer 8192 bytes, with a comment pointing here.
- Mitigation: when a new test fails, first ask "is the test right?"; fixtures state their size assumptions in comments.
- Regression: the test itself.
- Lesson: the bounds check worked exactly as designed: good sign for untrusted ELF input.

### BUG-0001: `noreturn` functions that "return"
- Found: 2026-11-02, W3-S11, first `make` with `-Werror`.
- Severity: minor (build break).
- Symptom: `'noreturn' function does return` for `panic` and `loader_main`.
- Root cause: they ended in `cpu_halt_forever()`, which was not itself marked `noreturn`, so GCC could not prove the end was unreachable.
- Fix: `__attribute__((noreturn))` on `cpu_halt_forever` in `loader/io.h`.
- Mitigation: keep `-Werror`; mark halt/panic/exit helpers `noreturn` everywhere (nucleus too).
- Regression: the build itself.
- Lesson: attributes are contracts the compiler can only check if they are complete down the call chain.

---

## Anticipated pitfalls (known before they bite)

Pre-registered hazards with planned mitigations. If one bites anyway, open a BUG and link the PIT id. Full list for Go hosting: [GO_RUNTIME_HOSTING.md §5](../02-architecture/GO_RUNTIME_HOSTING.md) (PIT-01..PIT-11).

| ID | Wave | Pitfall | Planned mitigation |
|---|---|---|---|
| PIT-20 | W4 | Enabling NX bit 63 in PTEs without EFER.NXE → #GP/#PF | set EFER.NXE before CR3 load; unit test flags |
| PIT-21 | W4 | Identity map removed while still executing low code | remove only after jumping to higher half |
| PIT-22 | W5 | Context switch forgets FS base or TSS.RSP0 | `switch_to` checklist + ktest |
| PIT-23 | W5 | Lost wake-up in futex (check and sleep not atomic) | hold the bucket lock across compare + enqueue |
| PIT-28 | W7 | Spurious IRQ 7/15 from the 8259 (no ISR bit set) | read the ISR (OCW3) and drop it without EOI (IRQ 15: EOI the master only) |
| PIT-29 | W7 | Lost interrupt: it fires before the driver waits | `pending` flag per line, set by the ISR, consumed by `irq_wait` |
| PIT-24 | W9 | SYSRET with non-canonical RCX (#GP in ring 0) | done in W9-S02: nu_user_syscall checks the return RIP and turns a non-canonical one into a fault request (an IRETQ fallback would #GP in ring 0 too); ktest uthread_noncanonical_return |
| PIT-25 | W9 | Kernel follows a user pointer into kernel memory | copyin/copyout validate ranges against user VMAs |
| PIT-26 | W10 | fork copies the Go kernel mappings as user-visible | kernel ranges always supervisor-only; ktest |
| PIT-27 | W11 | Framebuffer pitch != width*4 | always use `line_length` (done in the Doom snippet) |
| PIT-30 | W14 | RAM above 4 GiB and the PCI hole are new for the PMM and the physmap | done in W14-S01: the physmap maps only usable ranges above 4 GiB; host test `vm_physmap_skips_the_pci_hole`, and a 6 GiB guest by hand |
| PIT-31 | W14 | The mouse and keyboard share the 8042's data port: a byte read by the wrong side is lost | done in W14-S02: one drain under a mutex for both IRQs, routed by the status register's aux bit; the mouse starts after the keyboard |
| PIT-32 | W14 | QEMU sends PS/2 motion only while its window has grabbed the pointer | closed (W22-S05): QEMU's virtio tablet (`TABLET ?= 1` on the run targets) moves the pointer where the host's is, with no grab; paint, X and Qt follow it (`make tablet-smoke`). With `TABLET=0` the old advice holds: click into the window, Ctrl-Alt-G lets go |
| PIT-33 | W14 | Descriptors passed over sockets can form cycles (a socket sent over itself) | accepted: queued descriptors close when the receiving socket closes; Linux's garbage collector is not copied, a rare leak is |
| PIT-34 | W15 | A static qtbase is a long build and about 2 GiB on disk | built once into `~/.cache/burrow-qt` by `make qt`, never inside `make test`; host and target builds share one feature list (`COMMON` in `scripts/qt.sh`) so `moc` sees the same features |
| PIT-35 | W15 | Qt's cross build probes the host's libraries (pkg-config, system FreeType) | done in W15-S01: every library bundled (`-qt-freetype -qt-harfbuzz -qt-pcre -qt-zlib -qt-libpng`), `PKG_CONFIG_LIBDIR=/nonexistent`, a toolchain file with the musl sysroot; programs build with Qt's own `qt.toolchain.cmake` |
| PIT-36 | W15 | The census misses calls Qt makes only on Burrow, and calls strace cannot see | W15-S01 found one at once: musl reads `clock_gettime` through the vDSO, so it is listed by hand (`# vdso`); paths Burrow answers differently show in the `unimplemented syscall` log |
| PIT-37 | W15 | A Qt program (fonts, a 3 MiB backing store) runs in tens of MiB, and fork copies eagerly (`QProcess`) | 512 MiB since W14-S01; copy-on-write fork stays in the backlog |
| PIT-38 | W15 | The newest Qt may need a newer compiler than musl.cc's GCC 11.2.1 | checked in W15-S01: qtbase 6.12.0 builds with it |
| PIT-39 | W16 | Autotools cross builds of 18 X libraries probe the host | `--host=x86_64-linux-musl`, `PKG_CONFIG_LIBDIR` at our sysroot only, `--disable-shared` everywhere (W16-S01: all built at the first attempt; TinyX needed `ACLOCAL_PATH` for xtrans's macros) |
| PIT-40 | W16 | st needs pseudo-terminals, which Burrow does not have | a whole slice of tty work in W16-S03, with a `ptytest` checked against Linux |
| PIT-41 | W16 | TinyX's input is 2006 Linux: the console keymap through `KDGKBENT`, its own mouse protocol detection | answer as Linux did then; W14's `/dev/input/mice` already speaks the IntelliMouse handshake |
| PIT-42 | W16 | No XKB: Qt 6's xcb platform, GTK3 and XKB-built clients will not run on TinyX | ship only programs built without XKB; the 1.20-branch kdrive is E16, later |
| PIT-43 | W16 | Fork copies eagerly, and every st and dmenu forks a program | measured in W16-S05; copy-on-write fork is E18 |
| PIT-44 | W17 | DMA needs physical addresses that stay put; Go memory is virtual and filled on first touch (BUG-0012) | done in W17-S01: rings and buffers are nucleus frames (`driver.Env.DMAPage`), never Go heap |
| PIT-45 | W17 | The e1000's PCI INTx line reaches the 8259 as an ISA line (QEMU: 11) and may be shared | done in W17-S01: the line comes from PCI config space; the handler reads the cause register (ICR), so a shared line costs one read |
| PIT-46 | W17 | gVisor's module pulls in more than the stack, and grows the kernel | done in W17-S02: vendored at a pinned version (41 packages, 12 MB in `kernel/vendor`); the kernel grew from 3.5 to 6.5 MB; the census is unchanged; the `go` line it needs is pinned to go1.22's GODEBUG defaults |
| PIT-47 | W17 | The stack's goroutines and timers share Go's one P with the syscall dispatcher (BUG-0014) | measured in W17-S03: while 16 MiB cross loopback TCP (35 MB/s), the slowest `getppid` takes 3.2 ms (`nettest`'s `bulk`); slower, not starved |
| PIT-48 | W17 | Tests must not need the internet | every check in `make test` talks to the host through QEMU's user network (10.0.2.2) or `hostfwd`; HTTPS only in `make run-net` |
| PIT-49 | W18 | Copy-on-write is easy to get almost right: every kernel write into user memory must break the share | done in W18-S01: `CopyOut`, the ELF loader and `MADV_DONTNEED` clearing break it, `mprotect`/`mremap` keep the page read-only underneath; host tests write through each path after a fork; the dispatcher sends write faults on present pages to `FaultIn` |
| PIT-50 | W18 | A page made read-only in another process keeps a stale writable TLB entry | checked in W18-S01: Go runs with the kernel's CR3, user pages are not global, so a process's entries go when its CR3 loads again; unmap flushes the current space with `invlpg` |
| PIT-51 | W18 | Frames freed twice or never across fork, exit, `munmap`, `mremap`, `execve` and shared objects | one owner count per family (`frameRefs`); host tests count frames in use after every exit order; S03's shared objects join the same accounting |
| PIT-52 | W18 | Shared objects outlive processes (SysV segments until `IPC_RMID` and the last detach, `/dev/shm` files until unlinked, a memfd until its last descriptor and mapping) | S03/S04: an object frees its frames only when nothing names or maps it |
| PIT-53 | W18 | Dynamic programs read more of the system (`/etc/ld-musl-x86_64.path`, the library path, `MAP_FIXED` over their own reservations, `mprotect` of file mappings) | S05: a census of a dynamic program on Linux first |
| PIT-54 | W19 | A file system writer corrupts quietly: a wrong free count or a block in two files shows only later | S02: every write test ends with `e2fsck -fn`; random operation sequences are seeded and logged so a failure replays |
| PIT-55 | W19 | The reader and the writer agree on a mistake in the on-disk format (sizes that grew across revisions: `s_inode_size`, `i_size_high`) | done in W19-S01: the reader is tested only on volumes e2fsprogs made (`mke2fs -d`, `debugfs`, `e2fsck -D`), revision 0 and 1, 1 and 4 KiB blocks |
| PIT-56 | W19 | Every program has run as root, so a missing permission check never showed | S04: each check gets a test that fails without it; `permtest` and `unix-smoke` run as a user |
| PIT-57 | W19 | The path walker changes every path: `..` stops being textual | S05: the full `make test` before anything else; host tests for loops, links across mounts, `..` after a link |
| PIT-58 | W19 | Set-user-id programs are an attack surface (`LD_LIBRARY_PATH`, a set-uid file rewritten by its user) | S03/S04: `AT_SECURE` for set-id programs; a non-root write drops the set-uid bit, as Linux |
| PIT-59 | W19 | A program run from a disk reads its pages at first touch (W18-S02): it must not change under it | not done in W19 (an open risk, UNIX.md says so): `ETXTBSY` and keeping a deleted program's inode until its last mapping goes are for later |
| PIT-60 | W20 | Races one CPU hid: code that relied on interrupts-off for exclusion (scheduler, IRQ pending bits, `trap_last`, the uthread FIFO, the futex table) | S05 listed them (ADR-0011): already under spinlocks: `irq.c` lines, `futex.c` buckets, `shim_poll.c` files, `uspace.c` spaces, `uthread.c` request FIFO; per CPU now: `struct percpu`, GDT/TSS, LSTAR; written at boot only: `acpi_boot`, `fpu`, `protect`, `time`'s calibration, `g_kernel_cr3`, `g_handlers`; **unlocked, for S06**: `sched.c` (`g_threads` states, `g_idle`, `g_slice`, `g_switches`, `g_next_id`), `frames.c` (`g_pmm`, free bitmap), `vmm.c`'s kernel space, `trap_last`, `shim.c`'s report table, `time.c`'s `g_ticks`, `go_load.c`'s kargs, kprintf's output (no cross-CPU lock: APs never print). S06: the scheduler's state is under `g_sched_lock` (idle and slice per CPU), `frames.c` and `vmm.c` have spinlocks, kprintf a cross-CPU guard, `g_ticks` is CPU 0's only; still unlocked, and safe while Go and user threads stay on CPU 0: `shim.c`'s report table, `go_load.c`'s kargs; `trap_last` is written by whichever CPU takes an allowed fault (ktests read it from the faulting thread, one test at a time). Page-table changes flush only the changing CPU until S07. S07: shootdown on every CPU that may cache an entry (`tlb.c`); Go and user threads unpinned, so the shim's report table and the console `write` (now under kprintf's guard) see several CPUs. S08: the Go kernel on four Ps found BUG-0027 (two CPUs faulting on one page) |
| PIT-61 | W21 | The mode set ends text output: after 4F02 the BIOS teletype has no text mode | S01: `vbe_setup` is stage2's last real-mode step, after its last BIOS message; its own output is COM1 only (`vbe16.inc`); the loader and the nucleus never used the screen |
| PIT-62 | W21 | QEMU's VBE modes always have a tight pitch, so an odd resolution alone proves nothing about strides | S01: 4F06 asks for a padded scanline and stage2 keeps what the card answers (it rounds: 1700 became 1696); `make vbe-smoke` checks 5248 at 1280x1024x32 |
| PIT-63 | W21 | Pixel formats other than XRGB (BGR, 16 and 24 bpp) exist on real cards and not in QEMU's std-vga | S02 carries the colour fields in boot_info; S03 packs by them, with host tests for layouts QEMU never produces |
| PIT-64 | W21 | One physical range mapped with two memory types (WC and WB or UC) is undefined on hardware and silent in QEMU | S04: `vm_map_mmio_type` refuses a second kernel mapping with another type; programs' mmaps of `/dev/fb0` (write-back since W11, against the kernel's uncached mapping) are WC like the kernel's (`UPROT_WC`, `proc.ProtWC`, kept by mprotect and fork) |
| PIT-65 | W21 | The PAT must be the same on every CPU (W20 started the APs) | S04: the BSP after `protect_init`, each AP in `ap_main` before anything; ktest `smp_pat_everywhere` reads it back everywhere |
| PIT-66 | W21 | boot_info version skew: a half-updated mirror can pass the C asserts and misread the base in Go | S02: both versions bump, the loader refuses a mismatch, `kernel/bootinfo`'s layout test pins the offsets |
| PIT-67 | W21 | The DISPI fallback must stay a real path for images that ask for no mode, or whose mode does not exist | S01: disk.img and the ktest images carry no `video` payload; `vbe-none` boots on; S02's regression case keeps the Go side honest |
| PIT-68 | W21 | stage2's 16 KiB budget | S01: VBE took it from 1502 to 2056 bytes; `make sizes` each slice |
| PIT-69 | W22 | Shared interrupt lines: a handler that claims an interrupt it did not raise, or a line nobody claims while it stays asserted (level-triggered: a storm) | S01: `driver.Lines` calls every handler on a line, each reads its own device's status (`e1000.Interrupt` reports whether ICR was set); an unclaimed interrupt is counted and logged once |
| PIT-70 | W22 | DMA and memory ordering: the card reads buffers and descriptors the CPU just wrote, virtio's used ring is written by the device; the order before a doorbell matters, now on four CPUs | open (S02: the ring's descriptors written before the tail register; S04: virtqueue indices as atomic 32-bit words, published after the entries; S06 checks it on one CPU and four) |
| PIT-71 | W22 | Audio is real time and QEMU's TCG is not: a slow guest underruns | open (S02: a deep ring, an underrun a gap rather than a hang; WAV checks look for the sound and its pitch) |
| PIT-72 | W22 | OSS is old and loose: rates and formats the card lacks, `SNDCTL_DSP_*` numbers and `audio_buf_info` exactly Linux's | open (S02: checked against the host's `<sys/soundcard.h>`) |
| PIT-73 | W22 | Absolute and relative pointers do not mix: X accelerates relative moves, mousedev's stream is relative, the screen's size can change (W21) | closed (S05: `/dev/input/mice` moves from the position scaled to bga's current mode, a session starting at the centre and edges overshot to resynchronise; `xaccel` turns X's acceleration off; paint and Qt read the absolute axes themselves) |
| PIT-74 | W22 | virtio's legacy and modern faces: transitional devices also offer the legacy I/O BAR | closed (S04: `virtio.FindCaps` needs all four modern capabilities and `Negotiate` refuses a device without `VIRTIO_F_VERSION_1`, setting FAILED; both tested against a fake device) |
| PIT-75 | W22 | Harness timing: QMP's absolute events and the HMP monitor's keys are two sockets | closed (S05: the `abs` steps go through QMP and buttons through the monitor; keys files wait for a program's line before each, and `sendkeys.py` pauses 0.1 s after each `abs` step) |
| PIT-76 | W23 | `/proc` formats are a contract: busybox, musl and Go parse them by position and by key; a missing field or a different unit breaks them quietly | open (S01: `user/proctest` runs the same checks on the host's Linux; `stat`'s 52 fields counted in `procfs` tests and in proctest) |
| PIT-77 | W23 | Snapshots and locks: procfs reads every process, which may exit between a listing and an open | open (S01: `s.mu`, then `p.mu`, then the memory's lock, one at a time; a vanished pid is `ErrNotExist`; `TestGoneProcess`) |
| PIT-78 | W23 | Links that depend on the caller: `self` and `fd/N` must read as the walking process's, never cached for another, and reopening `fd/N` must check the target's owner | closed (S01: `vfs.ResolveAs`/`ReadLinkAs` carry the `Caller`; S03: procfs reads every link per caller, nothing is cached; `fd/N`, `exe`, `cwd` and `root` need `MayTrace` and the fd directories are 0500; a pathless `fd/N` is shared only after the same check; `TestProcFDLinks`) |
| PIT-79 | W23 | CPU-time counters change on every CPU at every switch: reads must not tear and the cost must stay out of `syslat` | closed (S02: one writer per counter, the CPU running the thread with IF=0; counters only grow and are read with 64-bit loads; a clock read and a few adds per switch, tick and ring crossing; `mem-smoke`'s `syslat` in budget) |
| PIT-80 | W23 | virtio-blk's chains and memory ordering: data pages the device writes, many requests in flight on four CPUs | closed (S04: each slot owns its descriptors and DMA pages and goes back only after the device returned it (a request lost to a timeout keeps its slot out); descriptors are written before the available index, the used index is read before the status; `TestManyReaders` under `-race`; `virtio-smoke` on four CPUs) |
| PIT-81 | W23 | Two disk buses, one set of labels: the same label on an IDE and a virtio disk must be refused, not mounted twice | closed (S04: `mountData` remembers each label and logs an error for the second disk that carries it, leaving it unmounted) |
| PIT-82 | W23 | DHCP and a fixed network: slirp always answers 10.0.2.15, so tests must not mistake the fixed configuration for a lease, and a lease that never comes must time out | closed (S05: the lease has its own marker, `eth0 dhcp ... lease ... from`, that a fixed configuration never prints, and virtio-net-smoke checks it; no answer in four tries of 2 s is `ErrNoLease` and the fixed addresses are used, `TestNoServer`) |
| PIT-83 | W24 | Ports bring system calls nobody served yet: dropbear and NetSurf are the first big network programs in C | open (S01: `census-net.sh` before the first boot; curl and wget needed nothing new; S02: `census-ssh.sh` found `set_robust_list` and `execveat`) |
| PIT-84 | W24 | ssh needs every piece of the Unix at once: `crypt`, shadow passwords, `setgroups`/`setgid`/`setuid` in order, a controlling pty, a session leader | open (S02, S03) |
| PIT-85 | W24 | Entropy and keys: `kernel/random` was "not for cryptography"; host keys and TLS need good random numbers | closed (S02: said so: ChaCha8 seeded from RDRAND, which QEMU's CPU has, fit for a test network and no more; host keys are made by `dropbear -R` on the machine, never shipped) |
| PIT-86 | W24 | TLS on TCG is slow: handshakes do big-number arithmetic under emulation | open (S01: curl's handshakes finish well inside the smoke's waits; budgets measured in S06) |
| PIT-87 | W24 | NetSurf is big: many libraries, a host tool, a build system of its own | open (S04: a spike with a budget and links2 as the fallback) |
| PIT-88 | W24 | Two TLS stacks, one test CA: busybox's own TLS and mbedTLS accept different things | open (S01: tls-server.py's default context suits both; busybox's checks no certificate, so only curl's refusals are tested) |
| PIT-61 | W20 | A lost wake-up across CPUs: check-then-block not under the waker's lock (PIT-23's rule with a real second CPU) | S06: ktests ping-pong across CPUs millions of times |
| PIT-62 | W20 | Stale TLB entries on other CPUs: copy-on-write breaks silently, a reused frame is corrupted | S07: shootdown before any frame is reused; ktests aim at exactly this |
| PIT-63 | W20 | The AP trampoline is 16-bit code in a page the loader must leave alone | S05: the page in the memory map and MEMORY_MAP.md; a regression case corrupts it and must fail loudly |
| PIT-64 | W20 | XSAVE area size and alignment (CPUID-sized, 64-byte aligned, copied whole by clone) | done in W20-S03: 1024 bytes per thread, 64-byte aligned, CPUID leaf 0xD checked against it at boot (panic if larger); a fork child gets the legacy part with the header marked |
| PIT-65 | W20 | SMAP finds the nucleus touching user memory | done in W20-S02: none found; the kernels reach user memory only through the physmap, and the full `make test` passes with SMAP on; `ktest_protect.c` proves an unbracketed access faults |
| PIT-66 | W20 | Multi-threaded TCG changes timing everywhere | every keys file waits for text, not time; budgets measured again on four CPUs (S08) |
| PIT-67 | W20 | Go's runtime on many Ps: more Ms in the nucleus, SIGURG preemption aimed at threads on other CPUs, parallel GC | S06-S08: an IPI stops a thread at its next return; the census is rerun |
