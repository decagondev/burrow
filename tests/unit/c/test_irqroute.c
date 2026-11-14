/* irqroute: ISA lines through the I/O APIC (W27-S01). The MADT's overrides
 * win, "conforms" falls back to the 8259's edge/level register, a level
 * line with no stated polarity is active low; redirection entries and MSI
 * messages carry the vector, the destination and the mode bits where the
 * 82093AA datasheet and the SDM put them. */
#include <string.h>
#include "minitest.h"
#include "irqroute.h"

static struct acpi_info qemu_like(void) {
    struct acpi_info a;
    memset(&a, 0, sizeof a);
    for (unsigned i = 0; i < ACPI_ISA_LINES; i++) a.isa_gsi[i] = i;
    a.isa_gsi[0] = 2;                                         /* the PIT */
    a.isa_flags[9] = ACPI_INTI_TRIG_LEVEL | ACPI_INTI_POL_HIGH;  /* QEMU's PCI lines */
    a.isa_flags[11] = ACPI_INTI_TRIG_LEVEL | ACPI_INTI_POL_HIGH;
    a.isa_flags[3] = ACPI_INTI_TRIG_EDGE | ACPI_INTI_POL_LOW;
    return a;
}

TEST(irqroute_isa_defaults) {
    struct irq_route r;
    irq_route_isa(0, 4, 0, &r);                     /* no MADT: identity, edge, high */
    CHECK(r.gsi == 4 && !r.level && !r.active_low);
    struct acpi_info a = qemu_like();
    irq_route_isa(&a, 1, 0, &r);
    CHECK(r.gsi == 1 && !r.level && !r.active_low);
}

TEST(irqroute_overrides) {
    struct acpi_info a = qemu_like();
    struct irq_route r;
    irq_route_isa(&a, 0, 0, &r);
    CHECK(r.gsi == 2 && !r.level);
    irq_route_isa(&a, 11, 0, &r);                   /* stated: level, active high */
    CHECK(r.gsi == 11 && r.level && !r.active_low);
    irq_route_isa(&a, 3, 1u << 3, &r);              /* stated edge beats the ELCR */
    CHECK(!r.level && r.active_low);
}

TEST(irqroute_elcr) {
    struct acpi_info a = qemu_like();
    struct irq_route r;
    irq_route_isa(&a, 10, 1u << 10, &r);            /* conforms: the ELCR says level */
    CHECK(r.gsi == 10 && r.level && r.active_low);  /* PCI's polarity */
    irq_route_isa(&a, 10, 0, &r);
    CHECK(!r.level && !r.active_low);
}

TEST(irqroute_entry_and_msi) {
    struct irq_route edge = {2, 0, 0}, level = {11, 1, 1};
    CHECK_EQ_U64(ioapic_entry(0x20, 0, &edge, 0), 0x20ull);
    CHECK_EQ_U64(ioapic_entry(0x2B, 3, &level, 1),
                 0x2Bull | IOREDTBL_LEVEL | IOREDTBL_ACTIVE_LOW | IOREDTBL_MASKED | (3ull << 56));
    uint64_t addr;
    uint32_t data;
    msi_message(0, 0x50, &addr, &data);
    CHECK_EQ_U64(addr, 0xFEE00000ull);
    CHECK(data == 0x50);
    msi_message(5, 0x6F, &addr, &data);
    CHECK_EQ_U64(addr, 0xFEE05000ull);
    CHECK(data == 0x6F);
}
