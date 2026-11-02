/* gdt.h - the nucleus's own GDT and its single TSS.
 *
 * Selector order is fixed by SYSCALL/SYSRET (STAR MSR), BOOT_PROTOCOL.md §7:
 *   SYSCALL loads CS = STAR[47:32] = 0x08, SS = +8  = 0x10
 *   SYSRET  loads CS = STAR[63:48] + 16 = 0x2B, SS = +8 = 0x23  (base 0x18)
 * IST stacks: an exception or IRQ never runs on whatever stack it interrupted
 * (possibly a small Go goroutine stack); trap entry moves to the thread's
 * kernel stack afterwards (trap_entry.asm). */
#ifndef NUCLEUS_GDT_H
#define NUCLEUS_GDT_H
#include <stdint.h>

#define SEL_KCODE   0x08
#define SEL_KDATA   0x10
#define SEL_UCODE32 0x18   /* placeholder: null; only defines the SYSRET base */
#define SEL_UDATA   0x23   /* 0x20 | RPL 3 */
#define SEL_UCODE   0x2B   /* 0x28 | RPL 3 */
#define SEL_TSS     0x30   /* 16-byte system descriptor */

#define IST_IRQ 1          /* every vector except the two below */
#define IST_DF  2          /* #DF: must work even if IST1 is what overflowed */
#define IST_NMI 3          /* NMI can arrive in the middle of any trap entry */

#define IST_STACK_BYTES 16384u

/* Loads the GDT, reloads every segment register and the task register. */
void gdt_init(void);
/* Stack the CPU switches to on a ring 3 -> ring 0 transition (W9). */
void tss_set_rsp0(uint64_t rsp0);
#endif
