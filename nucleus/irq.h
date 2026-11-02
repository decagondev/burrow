/* irq.h - ISA interrupt lines as thread wake-ups (ADR-0005; the Go side calls
 * them as nu_irq_wait 0x1000 / nu_irq_ack 0x1001, NUCLEUS_ABI.md).
 *
 * The ISR never runs driver code: it masks the line, sends EOI, records the
 * interrupt as pending and wakes the one thread waiting on the line. The
 * driver drains its device, then acks, which unmasks the line again. An
 * interrupt that arrives while nobody waits stays pending (PIT-29), so the
 * usual loop "wait, handle, ack" loses nothing.
 *
 * Lines 0-15 except 2 (the 8259 cascade). Must never block in the ISR. */
#ifndef NUCLEUS_IRQ_H
#define NUCLEUS_IRQ_H
#include <stdint.h>

#define IRQ_LINES 16u

void irq_init(void);                 /* after sched_init: installs the vector 0x20-0x2F handlers */
long irq_wait(uint64_t line);        /* 0; -EINVAL bad line; -EBUSY another thread waits */
long irq_ack(uint64_t line);         /* 0; unmasks a line irq_wait has armed */

/* For ktests. */
int      irq_pending(uint64_t line);
uint64_t irq_count(uint64_t line);   /* interrupts delivered so far */
uint64_t irq_spurious(void);         /* spurious IRQ 7/15 dropped */
#endif
