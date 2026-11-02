/* frames.c - see frames.h. Interrupts are off around each list operation, so
 * an IRQ handler may allocate too (one CPU; W5-S11 adds the spinlock). */
#include "frames.h"
#include "pmm.h"
#include "boot.h"
#include "cpu.h"
#include "kprintf.h"
#include "lib_string.h"
#include "panic.h"

#define LOW_MEMORY_END 0x100000ull   /* firmware, boot_info, E820, manifest, loader */

static struct pmm g_pmm;
static struct pmm_range g_reserved[BOOT_MODULES_MAX + 1];
static uint32_t g_nreserved;

static void *physmap_ptr(void *env, uint64_t pa) { (void)env; return phys_to_virt(pa); }

void frames_init(void) {
    g_reserved[g_nreserved++] = (struct pmm_range){ 0, LOW_MEMORY_END };
    uint32_t nmods;
    const struct boot_module *mods = boot_modules(&nmods);
    for (uint32_t i = 0; i < nmods; i++) {
        uint64_t start = mods[i].phys_start & ~(PMM_FRAME - 1);
        uint64_t end = (mods[i].phys_start + mods[i].size_bytes + PMM_FRAME - 1) & ~(PMM_FRAME - 1);
        g_reserved[g_nreserved++] = (struct pmm_range){ start, end - start };
    }
    uint32_t n;
    const struct e820_entry *map = boot_e820(&n);
    pmm_init(&g_pmm, physmap_ptr, 0);
    pmm_add_e820(&g_pmm, map, n, g_reserved, g_nreserved);
    if (g_pmm.free_frames == 0) panic("no free physical memory");
    kprintf("[nucleus] pmm ok (%llu MiB free in %llu frames)\n",
            (unsigned long long)(g_pmm.free_frames * PMM_FRAME >> 20),
            (unsigned long long)g_pmm.free_frames);
}

uint64_t frame_alloc(void) {
    uint64_t f = irq_save();
    uint64_t pa = pmm_alloc(&g_pmm);
    irq_restore(f);
    return pa;
}

uint64_t frame_alloc_zeroed(void) {
    uint64_t pa = frame_alloc();
    if (pa) memset(phys_to_virt(pa), 0, PMM_FRAME);
    return pa;
}

void frame_free(uint64_t pa) {
    if (pa & (PMM_FRAME - 1)) panic("frame_free: unaligned %llx", (unsigned long long)pa);
    if (frame_is_reserved(pa)) panic("frame_free: %llx is reserved", (unsigned long long)pa);
    uint64_t f = irq_save();
    pmm_free(&g_pmm, pa);
    irq_restore(f);
}

uint64_t frames_free_count(void)  { return g_pmm.free_frames; }
uint64_t frames_total_count(void) { return g_pmm.total_frames; }

int frame_is_reserved(uint64_t pa) {
    for (uint32_t i = 0; i < g_nreserved; i++)
        if (pa < g_reserved[i].base + g_reserved[i].size_bytes && g_reserved[i].base < pa + PMM_FRAME)
            return 1;
    return 0;
}
