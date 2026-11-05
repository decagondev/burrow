/* apic.h - interrupt controllers. The legacy 8259 pair is remapped to
 * 0x20-0x2F (so a stray IRQ cannot look like an exception) and starts fully
 * masked; irq.c unmasks single ISA lines for drivers (W7). It reaches the CPU
 * through LAPIC LINT0 in ExtINT mode ("virtual wire"), so ISA IRQs take their
 * EOI at the 8259, never at the LAPIC. Local APIC: timer, self-IPIs,
 * spurious vector 0xFF (no EOI). */
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
#define LAPIC_LVT_LINT0 0x350
#define LAPIC_TIMER_INIT  0x380
#define LAPIC_TIMER_COUNT 0x390
#define LAPIC_TIMER_DIV   0x3E0

void     pic_disable(void);
void     pic_set_masked(unsigned line, int masked);   /* call with IRQs off */
void     pic_eoi(unsigned line);
int      pic_in_service(unsigned line);
void     lapic_init(void);              /* after vmm_init: maps the MMIO page */
uint32_t lapic_read(uint32_t reg);
void     lapic_write(uint32_t reg, uint32_t v);
void     lapic_eoi(void);
void     lapic_self_ipi(uint8_t vector);
void     lapic_init_cpu(void);          /* on an AP (smp.c) */
void     lapic_send_ipi(uint8_t apic_id, uint32_t icr_lo);
#endif
