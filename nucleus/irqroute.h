/* irqroute.h - where an interrupt goes, worked out without touching hardware
 * (W27-S01): an ISA line's global system interrupt, trigger and polarity
 * from the MADT's overrides and the 8259's edge/level register, the I/O
 * APIC redirection entry for it, and the message a PCI device writes for
 * MSI. Pure: no io.h, no cpu.h; host-tested (tests/unit/c/test_irqroute.c).
 * Must never guess a polarity the MADT stated. Intel 82093AA datasheet
 * §3.2.4; ACPI 6.5 §5.2.12.5; Intel SDM vol. 3 §11.11 (MSI). ADR-0014. */
#ifndef NUCLEUS_IRQROUTE_H
#define NUCLEUS_IRQROUTE_H
#include <stdint.h>
#include "acpi.h"

struct irq_route {
    uint32_t gsi;          /* the I/O APIC input (global system interrupt) */
    uint8_t  level;        /* 1: level-triggered, masked until the driver acks */
    uint8_t  active_low;
};

/* irq_route_isa: line's route. elcr is the 8259's edge/level control
 * register (ports 0x4D0/0x4D1, bit n = line n is level): the firmware sets
 * it for lines PCI devices share, and it decides when the MADT says
 * "conforms to the bus". A level line with no stated polarity is active
 * low, as PCI's are; QEMU's MADT states active high for its own. */
void irq_route_isa(const struct acpi_info *a, unsigned line, uint16_t elcr, struct irq_route *out);

#define IOREDTBL_MASKED     (1ull << 16)
#define IOREDTBL_LEVEL      (1ull << 15)
#define IOREDTBL_ACTIVE_LOW (1ull << 13)

/* The 64-bit redirection entry: fixed delivery, physical destination. */
uint64_t ioapic_entry(uint8_t vector, uint8_t dest_apic, const struct irq_route *r, int masked);

#define MSI_ADDRESS_BASE 0xFEE00000ull

/* The MSI address and data for vector on the CPU with dest_apic: fixed,
 * edge, physical destination. */
void msi_message(uint8_t dest_apic, uint8_t vector, uint64_t *addr, uint32_t *data);
#endif
