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

*(none)*

## Closed

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
| PIT-32 | W14 | QEMU sends PS/2 motion only while its window has grabbed the pointer | open: click into the window (Ctrl-Alt-G lets go); an absolute pointer (vmmouse, USB tablet) is for later |
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
| PIT-46 | W17 | gVisor's module pulls in more than the stack, and grows the kernel | vendor a pinned version, import only `pkg/tcpip` and what it needs; watch the kernel's size and the runtime census (W17-S02) |
| PIT-47 | W17 | The stack's goroutines and timers share Go's one P with the syscall dispatcher (BUG-0014) | measure a busy socket against syscall latency in W17-S03 |
| PIT-48 | W17 | Tests must not need the internet | every check in `make test` talks to the host through QEMU's user network (10.0.2.2) or `hostfwd`; HTTPS only in `make run-net` |
