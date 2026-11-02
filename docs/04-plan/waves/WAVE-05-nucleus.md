# W5 Nucleus Core  (M5)  — status: planned

Goal: a robust ring-0 base. Everything Go will need in W6 exists and is self-tested.

### [x] W5-S01 Nucleus console + kprintf reuse
Move `kprintf.c`, `lib_string.c`, `crc32.c`, `char_sink.h` into `common/` built for both loader and nucleus (DRY). Test: all unit tests still pass.

### [x] W5-S02 GDT + TSS with IST stacks (E04.F01)
Selectors: 0x08 kcode, 0x10 kdata, 0x18 udata32 placeholder, 0x20 udata (0x23 RPL3), 0x28 ucode (0x2B RPL3), 0x30 TSS (16 bytes). Order chosen for SYSRET (STAR layout); document in BOOT_PROTOCOL. IST1 = interrupts, IST2 = #DF, IST3 = NMI.

### [x] W5-S03 IDT + exception stubs (E04.F01)
Generated table of 256 asm stubs (macro), common handler saving all registers into `struct trap_frame`. Handler prints vector name, error code, CR2, RIP and halts with debug-exit failure.
Test: `ktest` boot flag triggers `ud2` → expected `[trap] #UD at` marker (regression REG-0005).

### [x] W5-S04 `ktest` runner (E00.F02.S04)
A manifest entry `ktest` (a tiny text file) selects which in-kernel tests run; each prints `[ktest] name ok`. Smoke expect file per suite.

### [x] W5-S05 PIC remap + mask all, LAPIC enable (E04.F04)
Remap 8259 to 0x20-0x2F then mask (we use the LAPIC). Spurious vector 0xFF.

### [x] W5-S06 PMM (E04.F02)
Free-list of 4 KiB frames built from boot_info modules + E820. Pure core unit-tested on host. ktest: allocate all, free all, counts match.

### [x] W5-S07 VMM mechanism (E04.F03)
`vm_map/unmap/protect/translate` on any PML4 via physmap; `invlpg`. ktest: map a page, write, translate, unmap, access → #PF caught by a test hook.

### [x] W5-S08 Reserve/commit regions (E04.F03, PIT-05)
Region list per address space: reserved (no frames) vs committed. ktest: reserve 2 GiB on a 128 MiB VM succeeds; committing 4 KiB uses exactly 1 frame (+ table frames).

### [x] W5-S09 Time (E04.F04)
Calibrate TSC with PIT channel 2; LAPIC timer periodic 1 kHz; `time_monotonic_ns()`. ktest: busy-wait 100 ms is within 5%.

### [ ] W5-S10 Threads + context switch (E04.F05)
`struct thread { rsp, kstack, fs_base, state, ... }`; `switch_to` in asm saves callee-saved regs, switches RSP, writes FS_BASE, updates TSS.RSP0 and per-CPU kstack top. Round-robin preemptive on timer tick. Idle thread `sti; hlt`.
ktest: two threads print alternately 5 times.

### [ ] W5-S11 Spinlocks + futex (E04.F06)
IRQ-saving spinlocks; futex hash table of wait queues; timeout via sleep queue. ktest: ping-pong 10,000 round trips; wait with timeout returns `-ETIMEDOUT`.

### [ ] W5-S12 SSE enable + FPU state save (prep for Go)
CR0/CR4 bits; `fxsave/fxrstor` per thread (512-byte aligned area). ktest: two threads with different xmm0 values keep them across switches.

Exit: M5 demo; all ktests green in smoke.
