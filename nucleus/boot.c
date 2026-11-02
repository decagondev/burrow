#include "boot.h"
#include "kprintf.h"
#include "lib_string.h"
#include "panic.h"

#define NUCLEUS_VIRT_BASE 0xFFFFFFFF80000000ull   /* nucleus/linker.ld KERNEL_VMA */

static const struct boot_info *g_bi;

void *phys_to_virt(uint64_t pa) { return (void *)(uintptr_t)(g_bi->physmap_base + pa); }
const struct boot_info *boot_info(void) { return g_bi; }

/* bi is a physmap address; every address INSIDE it is physical. */
void boot_init(struct boot_info *bi) {
    if ((uint64_t)(uintptr_t)boot_init < NUCLEUS_VIRT_BASE) panic("not running in the higher half");
    if (bi->magic != BOOTINFO_MAGIC)     panic("boot_info magic mismatch");
    if (bi->version != BOOTINFO_VERSION) panic("boot_info version is not 2");
    if (bi->size != sizeof *bi)          panic("boot_info size mismatch");
    if ((uint64_t)(uintptr_t)bi != bi->physmap_base + BOOTINFO_PHYS_ADDR) panic("boot_info not in the physmap");
    if (bi->module_count == 0 || bi->module_count > BOOT_MODULES_MAX) panic("bad module count");
    g_bi = bi;

    uint32_t n;
    const struct boot_module *mods = boot_modules(&n);
    for (uint32_t i = 0; i < n; i++)
        kprintf("  module %08llx - %08llx  %s\n", (unsigned long long)mods[i].phys_start,
                (unsigned long long)(mods[i].phys_start + mods[i].size_bytes), mods[i].name);
    kprintf("[nucleus] boot_info v2 ok (physmap=%llx, modules=%u, e820 entries=%u)\n",
            (unsigned long long)bi->physmap_base, bi->module_count, bi->e820_count);
}

const struct boot_module *boot_modules(uint32_t *count) {
    *count = g_bi->module_count;
    return phys_to_virt(g_bi->modules_addr);
}

const struct e820_entry *boot_e820(uint32_t *count) {
    *count = g_bi->e820_count;
    return phys_to_virt(g_bi->e820_addr);
}

const struct boot_module *boot_module_find(const char *name) {
    uint32_t n;
    const struct boot_module *mods = boot_modules(&n);
    for (uint32_t i = 0; i < n; i++)
        if (strncmp(mods[i].name, name, sizeof mods[i].name) == 0) return &mods[i];
    return 0;
}
