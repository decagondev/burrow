/* irqroute.c - see irqroute.h (W27-S01). Pure arithmetic over the MADT's
 * answers; never reads a port or a register itself. */
#include "irqroute.h"

void irq_route_isa(const struct acpi_info *a, unsigned line, uint16_t elcr, struct irq_route *out) {
    uint16_t flags = 0;
    out->gsi = line;
    if (a && line < ACPI_ISA_LINES) {
        out->gsi = a->isa_gsi[line];
        flags = a->isa_flags[line];
    }
    uint16_t trig = flags & ACPI_INTI_TRIG_MASK, pol = flags & ACPI_INTI_POL_MASK;
    if (trig == ACPI_INTI_TRIG_LEVEL) out->level = 1;
    else if (trig == ACPI_INTI_TRIG_EDGE) out->level = 0;
    else out->level = line < 16 && (elcr >> line) & 1;
    if (pol == ACPI_INTI_POL_LOW) out->active_low = 1;
    else if (pol == ACPI_INTI_POL_HIGH) out->active_low = 0;
    else out->active_low = out->level;
}

uint64_t ioapic_entry(uint8_t vector, uint8_t dest_apic, const struct irq_route *r, int masked) {
    uint64_t e = vector | ((uint64_t)dest_apic << 56);
    if (r->level) e |= IOREDTBL_LEVEL;
    if (r->active_low) e |= IOREDTBL_ACTIVE_LOW;
    if (masked) e |= IOREDTBL_MASKED;
    return e;
}

void msi_message(uint8_t dest_apic, uint8_t vector, uint64_t *addr, uint32_t *data) {
    *addr = MSI_ADDRESS_BASE | ((uint64_t)dest_apic << 12);
    *data = vector;
}
