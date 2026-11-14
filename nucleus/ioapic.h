/* ioapic.h - the I/O APIC's registers (W27-S01): an index register at
 * +0x00 and a window at +0x10 in one MMIO page, a redirection entry per
 * input pin. irq.c decides what goes in each entry (irqroute.c works it
 * out); this file only reads and writes them. Callers hold irq.c's lock:
 * the index register is shared state. Never unmasks a pin it was not told
 * to. Intel 82093AA datasheet §3.2; ADR-0014. */
#ifndef NUCLEUS_IOAPIC_H
#define NUCLEUS_IOAPIC_H
#include <stdint.h>

/* Maps the I/O APIC at pa whose first input is gsi_base and masks every
 * pin. Returns the number of pins, 0 if nothing answers there. */
uint32_t ioapic_init(uint64_t pa, uint32_t gsi_base);
int      ioapic_has(uint32_t gsi);                       /* gsi is one of its pins */
void     ioapic_write_entry(uint32_t gsi, uint64_t entry);
void     ioapic_set_masked(uint32_t gsi, int masked);
uint64_t ioapic_read_entry(uint32_t gsi);                /* for ktests */
#endif
