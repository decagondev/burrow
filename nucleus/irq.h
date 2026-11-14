/* irq.h - interrupt lines as thread wake-ups (ADR-0005; the Go side calls
 * them as nu_irq_wait 0x1000 / nu_irq_ack 0x1001, nu_irq_alloc_msi 0x102E,
 * nu_irq_route 0x102F; NUCLEUS_ABI.md).
 *
 * The ISR never runs driver code: it records the interrupt as pending,
 * wakes the one thread waiting on the line and sends EOI; a level-triggered
 * line is masked until the driver acks it. An interrupt that arrives while
 * nobody waits stays pending (PIT-29), so the usual loop "wait, handle, ack"
 * loses nothing.
 *
 * Lines 0-15 are the ISA lines (not 2, the 8259's cascade): through the I/O
 * APIC when the MADT names one (W27-S01, ADR-0014), else through the 8259 in
 * virtual-wire mode; kargs "irq=pic" keeps the 8259. Lines 16-47 are MSI
 * lines, handed out one at a time. Must never block in the ISR. */
#ifndef NUCLEUS_IRQ_H
#define NUCLEUS_IRQ_H
#include <stdint.h>

#define IRQ_LINES     16u                 /* ISA */
#define IRQ_MSI_FIRST IRQ_LINES
#define IRQ_MSI_LINES 32u
#define VEC_MSI_BASE  0x50                /* MSI line n on VEC_MSI_BASE + n - 16 */

/* nu_irq_route's answer: kind in bits 7:0, then the flags, the GSI or
 * vector in bits 31:16. */
enum { IRQ_KIND_NONE = 0, IRQ_KIND_PIC = 1, IRQ_KIND_IOAPIC = 2, IRQ_KIND_MSI = 3 };
#define IRQ_ROUTE_LEVEL      (1u << 8)
#define IRQ_ROUTE_ACTIVE_LOW (1u << 9)
#define IRQ_ROUTE_SHIFT      16u

void irq_init(void);                 /* after sched_init: installs the handlers, routes the lines */
long irq_wait(uint64_t line);        /* 0; -EINVAL bad line; -EBUSY another thread waits */
long irq_ack(uint64_t line);         /* 0; unmasks a line irq_wait has armed */
/* A new MSI line: its number, the address and data its device writes. */
long irq_alloc_msi(uint64_t *addr, uint32_t *data);   /* -ENOSPC when all are taken */
long irq_route(uint64_t line);       /* the encoding above; -EINVAL bad line */

/* For ktests. */
int      irq_pending(uint64_t line);
uint64_t irq_count(uint64_t line);   /* interrupts delivered so far */
uint64_t irq_spurious(void);         /* spurious IRQ 7/15 dropped */
int      irq_uses_ioapic(void);
#endif
