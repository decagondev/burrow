# ADR-0011: Per-CPU state through GS, and how the other CPUs start

- **Status:** Accepted
- **Date:** 2026-11-05
- **Wave:** W20 (S05; S06 builds the scheduling on it, recorded below)

## Context
Until W20 the nucleus had one CPU and one `struct percpu g_percpu`, read by
fixed offset from `trap.asm` and `syscall.asm` (the current thread's kernel
stack, the syscall scratch slot) and by C (`thread_current`). A second CPU
needs its own copy of all of it, plus its own GDT and TSS (RSP0 and the IST
stacks differ per CPU), its own LSTAR/STAR/FMASK, its own local APIC set-up,
and a way to get from the real mode it wakes up in to the kernel's page
tables.

Two facts constrain the entry paths:

- The SYSCALL instruction gives the nucleus no free register and no stack:
  `syscall_entry` parked RAX in the per-CPU scratch slot before it could even
  tell a user program from Go (by CR3). With per-CPU data behind GS that read
  needs a GS base that is already right, but for a user caller it is not: a
  ring-3 program can load GS (`mov gs, 0x23` sets its base to 0).
- Go's runtime and the nucleus run in ring 0 and call SYSCALL too, and Go
  kernel text sits in the lower half, like user programs, so RIP alone cannot
  tell them apart.

## Options
1. **A per-CPU array indexed by the local APIC id**, read at every entry
   (`rdmsr`/MMIO, then a table) - no segment games, but slow and it needs a
   register before there is one.
2. **GS, `swapgs` on every ring-3 entry and exit, one SYSCALL entry** -
   impossible: the single entry cannot know whether to `swapgs` before it has
   a register, and it has none.
3. **GS, `swapgs` on ring-3 entries and exits, two SYSCALL entries**: LSTAR
   points at `syscall_entry_user` (starts with `swapgs`) while a user thread
   is the CPU's current thread and at `syscall_entry_kernel` (GS already
   right) otherwise; the scheduler rewrites LSTAR when the kind changes.
4. GS without `swapgs` (ring 3 keeps the kernel's GS base) - what the wave
   plan first said; wrong, because ring 3 can reload GS.

## Decision
Option 3.

- `struct percpu` (`nucleus/percpu.h`) keeps its first four fields at their
  offsets and adds `self` (+32, so C reads `this_cpu()` with one `gs:` load),
  the CPU's index and APIC id, the kind LSTAR now points at, and the AP
  mailbox. `g_cpus[SMP_MAX_CPUS]`, entry 0 the BSP; `gdt_init_cpu` binds
  `GS_BASE` right after `gdt_load` (which zeroes it).
- Interrupts and exceptions: `trap_common` does `swapgs` when the saved CS is
  ring 3, `trap_resume` when it returns to ring 3. #DF and NMI
  (`trap_common_ist`, which never return) check `GS_BASE` itself: an NMI can
  land between a `swapgs` and the `iretq`. User GS bases are always 0 (the
  GDT's bases are 0, there is no LDT, FSGSBASE is off, `arch_prctl(ARCH_SET_GS)`
  is not offered), so `KERNEL_GS_BASE` never carries one thread's value to
  another.
- SYSCALL: two entries as above. A user thread never runs nucleus code that
  calls SYSCALL, and Go threads are never user threads, so the LSTAR a thread
  sees is always its own kind.
- Each CPU has its own GDT and TSS (`g_gdt[cpu]`, `g_tss[cpu]`). The BSP keeps
  its static IST stacks; each AP's idle stack and three IST stacks are mapped
  from frames in a region of their own, each with a guard page.
- Start-up: the trampoline (`nucleus/ap.asm`) is copied to physical `0x8000`
  (the SIPI vector 0x08), memory that stage2 used and nothing uses after boot.
  It goes from real mode straight to long mode (CR0.PE and PG at once) on a
  temporary PML4: the kernel's entries plus an identity map of the first
  2 MiB, built from frames below 4 GiB and freed afterwards. It loads the
  BSP's CR4 and EFER (so SMEP, SMAP, NX and OSXSAVE match) and jumps to
  `ap_main` on the AP's own stack, which switches to the kernel CR3 at once.
  The BSP sends INIT, waits 10 ms, then two SIPIs 200 us apart, and waits up to
  a second for the AP to check in, one AP at a time.
- APs run an idle loop: `sti; hlt` until a wake IPI (vector `0xF0`, EOI only),
  then run the function in their mailbox. They run no threads yet: the run
  queue stays one, used by the BSP, until S06 makes it shared under a lock.
  No AP timer yet either (S06).
- **Scheduling on every CPU (S06).** One run queue: `g_threads` under
  `g_sched_lock`, taken with IF=0 and held across `switch_to`; the thread
  switched to releases it (its own `schedule_locked` caller, or
  `thread_bootstrap` / `thread_start_frame` when it is new), so no CPU can
  pick a thread whose registers another is still saving, and a waker on
  another CPU finds a sleeper either still running or fully switched out.
  Lock order: an object's lock (futex bucket, IRQ lines, uthread FIFO), then
  `g_sched_lock`. Each CPU has an idle thread (an AP's boot context becomes
  its own) and a LAPIC timer from the BSP's calibration; every tick may
  preempt, CPU 0's also wakes sleepers and counts `time_ticks`. Making a
  thread runnable sends the wake IPI to one idle CPU that may run it. A
  thread carries `pin_cpu`: nucleus kernel threads run anywhere; main, and
  every thread made from a trap frame (the Go kernel's Ms, user threads),
  stay on CPU 0 until TLB shootdown (S07), since a page-table change is
  flushed on the changing CPU only. A kernel stack is freed at join only if
  the joining CPU is the only one that ever touched it; otherwise it stays
  mapped for its slot's next thread. The frame allocator and the page-table
  calls take their own spinlocks; kprintf's guard is a bounded cross-CPU
  spinlock.

## Consequences
- Every per-CPU read in asm is a `gs:` read; every C one is `this_cpu()`.
  `syscall_entry` no longer exists; the gate marker prints the kernel entry.
- A path that returns to ring 3 without `trap_resume`, or enters ring 0 from
  ring 3 without `trap_common`/`syscall_entry_user`, would leave GS wrong; there
  is none, and new ones must swap.
- The scheduler writes LSTAR when it switches between a user and a kernel
  thread; it is the only writer after `syscall_cpu_init`.
- S06 must lock what "interrupts off" protected before: PIT-60 lists the
  nucleus's globals that a second CPU running threads would race on.
- Since S06 a ktest thread may run on any CPU; a test that needs one CPU's
  view runs in main (pinned to CPU 0) or in a thread from a frame.
- Tests: `boot_to_nucleus` says `smp: 1 CPU online`; `make acpi-smoke` and the
  ktest suite `smp` run on `-smp 4`, where every CPU adds to a spinlocked
  counter through its mailbox.
