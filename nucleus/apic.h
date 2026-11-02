/* apic.h - interrupt controllers. The legacy 8259 pair is remapped to
 * 0x20-0x2F (so a stray IRQ cannot look like an exception) and fully masked;
 * all interrupts come from the local APIC. Spurious vector 0xFF (no EOI). */
#ifndef NUCLEUS_APIC_H
#define NUCLEUS_APIC_H
#include <stdint.h>

#define LAPIC_ID        0x020
#define LAPIC_VERSION   0x030
#define LAPIC_TPR       0x080
#define LAPIC_EOI       0x0B0
#define LAPIC_SVR       0x0F0
#define LAPIC_ICR_LO    0x300
#define LAPIC_ICR_HI    0x310
#define LAPIC_LVT_TIMER 0x320
#define LAPIC_TIMER_INIT  0x380
#define LAPIC_TIMER_COUNT 0x390
#define LAPIC_TIMER_DIV   0x3E0

void     pic_disable(void);
void     lapic_init(void);              /* after vmm_init: maps the MMIO page */
uint32_t lapic_read(uint32_t reg);
void     lapic_write(uint32_t reg, uint32_t v);
void     lapic_eoi(void);
void     lapic_self_ipi(uint8_t vector);
#endif
