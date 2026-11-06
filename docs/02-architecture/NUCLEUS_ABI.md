# Nucleus ABI (private calls from the Go kernel)

Linux numbers stop well below 0x1000, so nucleus-private calls live at `0x1000-0x1FFF`. They use the Linux register convention (`RAX` = number, args `RDI RSI RDX R10 R8 R9`, result in `RAX`, negative = `-errno`). Only callers whose RIP is inside Go kernel text may use them; from user space they return `-ENOSYS`.

| Nr | Name | Args | Returns | Wave |
|---|---|---|---|---|
| 0x1000 | `nu_irq_wait` | irq (ISA line 0-15, not 2) | 0 when the IRQ fired (at once if it fired while nobody waited); `-EINVAL`, `-EBUSY` if another thread waits | W7 (done: `nucleus/irq.c`) |
| 0x1001 | `nu_irq_ack` | irq | 0 (unmasks the line; the ISR masked it) | W7 (done) |
| 0x1002 | `nu_bootinfo` | buf, len | bytes copied of boot_info v2 (at most 128); the module table it points to is read through the physmap | W8 (done: `amd64.BootInfo`) |
| 0x1003 | `nu_physmap_base` | — | physmap virtual base | W6 (done: `amd64.PhysmapBase`) |
| 0x1004 | `nu_debug_exit` | code | ends a QEMU test run; where there is no debug-exit device, code 0x10 (the console's `poweroff`) turns the machine off through ACPI S5 (W20-S04), and the call returns 0 only if neither worked | W6 (done: `amd64.DebugExit`) |
| 0x1005 | `nu_mmio_map` | pa, len, flags | virtual address of pa (physmap + pa), mapped NX, uncached, or write-combining with flags `NU_MMIO_WC` (1, W21-S04; uncached without PAT); `-EINVAL` for len 0, over 256 MiB, past 2^46 or an unknown flag; `-EEXIST` if a page is already mapped with the other cache type | W8 (done: `amd64.MapMMIO`; `MapMMIOWC` W21-S04) |
| 0x1006 | `nu_mem_info` | buf | 16; writes `{total, free}` (uint64 bytes) of the RAM the frame allocator manages, for `sysinfo` (free includes the 32 MiB kept for the kernel) | W14 |
| 0x1007 | `nu_set_realtime` | unix_ns | 0; wall time is unix_ns now: from here on `clock_gettime` answers `CLOCK_REALTIME`, `_COARSE`, `_ALARM` and `CLOCK_TAI` with it, every other clock stays monotonic since boot. Go calls it once, from the CMOS clock | W14 |
| 0x1010 | `nu_as_create` | — | address-space handle (1..32); the upper half (nucleus, physmap, kernel stacks) is shared, the lower half empty | W9 |
| 0x1011 | `nu_as_map` | as, va, pa, len, prot | 0; user pages, prot bits 1 = write, 2 = execute, 4 = write-combining device memory (`UPROT_WC`, W21-S04; also in `nu_as_protect` and `nu_as_map_batch`; uncached without PAT) (read is implied); va, pa, len page-aligned, va + len <= 2^47 | W9 |
| 0x1012 | `nu_as_unmap` | as, va, len | 0; frames are not freed (they belong to whoever allocated them) | W9 |
| 0x1013 | `nu_as_translate` | as, va | pa or -EFAULT | W9 |
| 0x1014 | `nu_page_alloc` | count (1) | physical address of a zeroed frame, or -ENOMEM (also while only `USER_RESERVE_FRAMES`, 32 MiB, are free: they stay for the kernel, W14-S01) | W9 |
| 0x1015 | `nu_page_free` | pa, count (1) | 0 | W9 |
| 0x1016 | `nu_as_destroy` | as | 0, frees its page tables; -EBUSY while a thread still runs in it | W9 |
| 0x1017 | `nu_as_protect` | as, va, len, prot | 0; every mapped page of the range is mapped again, same frame, with prot (unmapped ones skipped): fork write-protects a whole range in one call | W18 |
| 0x1018 | `nu_as_map_batch` | as, entries, n | 0; maps n single pages from an array of `{va, pa, prot}` (24 bytes each, at most 4096), each checked as `nu_as_map` checks one; on an error the pages mapped so far stay | W18 |
| 0x1020 | `nu_uthread_create` | as, rip, rsp, fs | tid of a ring-3 thread, runnable at once | W9 |
| 0x1021 | `nu_syscall_wait` | buf, max | blocks until a user thread needs Go, then copies up to max 64-byte requests `{tid, nr, args[6]}` into buf and returns how many; nr >= `0xFFFF0000` is a fault (vector in the low byte; args: cr2, rip, error), `0xFFFF0100` an interrupt stop; since W10 answering a fault or interrupt resumes the thread (Go may have changed its registers) | W9 |
| 0x1022 | `nu_syscall_reply` | tid, result | 0, resumes the user thread with RAX = result; -ESRCH if it is not waiting | W9 |
| 0x1023 | `nu_uthread_kill` | tid | 0 once the thread is gone (it may be waiting, runnable or preempted in ring 3); frees its kernel stack | W9 |
| 0x1024 | `nu_uthread_set_fs` | tid, fs | 0; the thread's FS base (`arch_prctl(ARCH_SET_FS)` for TLS) | W9 |
| 0x1025 | `nu_uthread_regs` | tid, buf, write | 0; copies the waiting thread's registers to buf (write 0) or from it (write 1): 672 bytes = r15 r14 r13 r12 r11 r10 r9 r8 rbp rdi rsi rdx rcx rbx rax rip rflags rsp fs_base, one pad word, then the 512-byte FXSAVE area. A write is checked: user RIP/RSP/FS, RFLAGS cut to the user-settable bits with IF on, MXCSR reserved bits cleared. -EBUSY unless Go holds a request from it | W10 |
| 0x1026 | `nu_uthread_create_from` | as, buf | tid of a new ring-3 thread starting from that register image (fork) | W10 |
| 0x1027 | `nu_uthread_set_space` | tid, as | 0; the waiting thread continues in another address space (execve) | W10 |
| 0x1028 | `nu_uthread_interrupt` | tid | 0; a thread running in ring 3 stops at its next pass through the kernel and posts an interrupt request (nr `0xFFFF0100`, args: rip) so Go can deliver a signal | W10 |
| 0x1029 | `nu_thread_times` | tid, buf | 16; buf = {user, kernel} nanoseconds the thread has run (ring 3; the nucleus or the Go kernel), charged at every switch, tick and ring crossing, so never more than a tick behind; -ESRCH for no such thread | W23 |
| 0x102A | `nu_cpu_times` | buf, max | the number of online CPUs written (at most max): 32 bytes each, {user, kernel, idle, switches}; idle is the CPU's idle thread's time | W23 |

Rules: every new call gets a row here **before** code, a Go wrapper in `kernel/arch/amd64/nucleus.go`, and a nucleus self-test. Numbers are never reused.
