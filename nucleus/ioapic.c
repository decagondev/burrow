/* ioapic.c - see ioapic.h (W27-S01). Writes the high half of an entry
 * before the low one, so a pin is never unmasked with half an entry. */
#include "ioapic.h"
#include "irqroute.h"
#include "vmm.h"

#define IOREGSEL     0x00u
#define IOWIN        0x10u
#define IOAPICVER    0x01u
#define IOREDTBL     0x10u   /* pin n: registers 0x10 + 2n (low), + 1 (high) */
#define VER_MAX_SHIFT 16u

static volatile uint32_t *g_regs;
static uint32_t g_gsi_base, g_pins;

static uint32_t rd(uint32_t reg)             { g_regs[IOREGSEL / 4] = reg; return g_regs[IOWIN / 4]; }
static void     wr(uint32_t reg, uint32_t v) { g_regs[IOREGSEL / 4] = reg; g_regs[IOWIN / 4] = v; }

uint32_t ioapic_init(uint64_t pa, uint32_t gsi_base) {
    g_regs = vm_map_mmio(pa & ~0xFFFull, 0x1000);
    if (!g_regs) return 0;
    g_regs = (volatile uint32_t *)((volatile uint8_t *)g_regs + (pa & 0xFFF));
    uint32_t ver = rd(IOAPICVER);
    if (ver == 0xFFFFFFFFu) { g_regs = 0; return 0; }
    g_gsi_base = gsi_base;
    g_pins = ((ver >> VER_MAX_SHIFT) & 0xFF) + 1;
    for (uint32_t pin = 0; pin < g_pins; pin++) {
        wr(IOREDTBL + 2 * pin + 1, 0);
        wr(IOREDTBL + 2 * pin, (uint32_t)IOREDTBL_MASKED);
    }
    return g_pins;
}

int ioapic_has(uint32_t gsi) { return g_regs && gsi >= g_gsi_base && gsi - g_gsi_base < g_pins; }

void ioapic_write_entry(uint32_t gsi, uint64_t entry) {
    if (!ioapic_has(gsi)) return;
    uint32_t pin = gsi - g_gsi_base;
    wr(IOREDTBL + 2 * pin, (uint32_t)IOREDTBL_MASKED);      /* masked while it changes */
    wr(IOREDTBL + 2 * pin + 1, (uint32_t)(entry >> 32));
    wr(IOREDTBL + 2 * pin, (uint32_t)entry);
}

void ioapic_set_masked(uint32_t gsi, int masked) {
    if (!ioapic_has(gsi)) return;
    uint32_t reg = IOREDTBL + 2 * (gsi - g_gsi_base), lo = rd(reg);
    wr(reg, masked ? lo | (uint32_t)IOREDTBL_MASKED : lo & ~(uint32_t)IOREDTBL_MASKED);
}

uint64_t ioapic_read_entry(uint32_t gsi) {
    if (!ioapic_has(gsi)) return 0;
    uint32_t reg = IOREDTBL + 2 * (gsi - g_gsi_base);
    return rd(reg) | ((uint64_t)rd(reg + 1) << 32);
}
