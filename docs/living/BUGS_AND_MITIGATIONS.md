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
- Regression: none possible until W11; add an evdev→doom key-map unit test in E10.F02.
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
