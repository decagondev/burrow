# Nucleus ABI (private calls from the Go kernel)

Linux numbers stop well below 0x1000, so nucleus-private calls live at `0x1000-0x1FFF`. They use the Linux register convention (`RAX` = number, args `RDI RSI RDX R10 R8 R9`, result in `RAX`, negative = `-errno`). Only callers whose RIP is inside Go kernel text may use them; from user space they return `-ENOSYS`.

| Nr | Name | Args | Returns | Wave |
|---|---|---|---|---|
| 0x1000 | `nu_irq_wait` | irq (ISA line 0-15, not 2) | 0 when the IRQ fired (at once if it fired while nobody waited); `-EINVAL`, `-EBUSY` if another thread waits | W7 (done: `nucleus/irq.c`) |
| 0x1001 | `nu_irq_ack` | irq | 0 (unmasks the line; the ISR masked it) | W7 (done) |
| 0x1002 | `nu_bootinfo` | buf, len | bytes copied of boot_info v2 (at most 128); the module table it points to is read through the physmap | W8 (done: `amd64.BootInfo`) |
| 0x1003 | `nu_physmap_base` | — | physmap virtual base | W6 (done: `amd64.PhysmapBase`) |
| 0x1004 | `nu_debug_exit` | code | ends a QEMU test run; returns 0 where there is no debug-exit device | W6 (done: `amd64.DebugExit`) |
| 0x1005 | `nu_mmio_map` | pa, len | virtual address of pa (physmap + pa), mapped uncached and NX; `-EINVAL` for len 0, over 256 MiB or past 2^46 | W8 (done: `amd64.MapMMIO`) |
| 0x1010 | `nu_as_create` | — | address-space handle | W9 |
| 0x1011 | `nu_as_map` | as, va, pa, len, prot | 0 | W9 |
| 0x1012 | `nu_as_unmap` | as, va, len | 0 | W9 |
| 0x1013 | `nu_as_translate` | as, va | pa or -EFAULT | W9 |
| 0x1014 | `nu_page_alloc` | count | physical address | W9 |
| 0x1015 | `nu_page_free` | pa, count | 0 | W9 |
| 0x1020 | `nu_uthread_create` | as, rip, rsp, tls | tid | W9 |
| 0x1021 | `nu_syscall_wait` | ring_ptr | number of requests ready | W9 |
| 0x1022 | `nu_syscall_reply` | tid, result | 0, resumes the user thread | W9 |
| 0x1023 | `nu_uthread_kill` | tid | 0 | W10 |

Rules: every new call gets a row here **before** code, a Go wrapper in `kernel/arch/amd64/nucleus.go`, and a nucleus self-test. Numbers are never reused.
