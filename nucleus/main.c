/*
 * main.c - Burrow nucleus entry point (Wave 4 scope: "hello from the higher half").
 *
 * Runs at 0xFFFFFFFF80100000 on page tables the loader built. Today it only
 * proves the hand-off: it runs in the top 2 GiB, reads boot_info v2 and the
 * module list through the physmap, and stops. Wave 5 grows it into the real
 * nucleus (GDT/TSS/IDT, PMM, VMM, threads) by calling NEW modules.
 *
 * Shares the freestanding serial/kprintf/string code with the loader (built
 * twice, with -mcmodel=kernel here). Must never return.
 */
#include <stdint.h>
#include <burrow/boot_info.h>
#include "io.h"
#include "serial.h"
#include "kprintf.h"

#define NUCLEUS_VIRT_BASE 0xFFFFFFFF80000000ull   /* nucleus/linker.ld KERNEL_VMA */

static uint16_t g_com1;   /* serial_sink keeps a pointer to it */

void __attribute__((noreturn)) nucleus_main(struct boot_info *bi);

static void __attribute__((noreturn)) panic(const char *what) {
    kprintf("[nucleus] PANIC: %s\n", what);
    qemu_exit(QEMU_EXIT_FAILURE);
    cpu_halt_forever();
}

/* bi is a physmap address; every address INSIDE it is physical. */
static void check_boot_info(const struct boot_info *bi) {
    if ((uint64_t)(uintptr_t)nucleus_main < NUCLEUS_VIRT_BASE) panic("not running in the higher half");
    if (bi->magic != BOOTINFO_MAGIC)     panic("boot_info magic mismatch");
    if (bi->version != BOOTINFO_VERSION) panic("boot_info version is not 2");
    if (bi->size != sizeof *bi)          panic("boot_info size mismatch");
    if ((uint64_t)(uintptr_t)bi != bi->physmap_base + BOOTINFO_PHYS_ADDR) panic("boot_info not in the physmap");
    if (bi->module_count == 0 || bi->module_count > BOOT_MODULES_MAX) panic("bad module count");

    const struct boot_module *mods = (const struct boot_module *)(uintptr_t)(bi->physmap_base + bi->modules_addr);
    for (uint32_t i = 0; i < bi->module_count; i++)
        kprintf("  module %08llx - %08llx  %s\n", (unsigned long long)mods[i].phys_start,
                (unsigned long long)(mods[i].phys_start + mods[i].size_bytes), mods[i].name);
    kprintf("[nucleus] boot_info v2 ok (physmap=%llx, modules=%u, e820 entries=%u)\n",
            (unsigned long long)bi->physmap_base, bi->module_count, bi->e820_count);
}

void __attribute__((noreturn)) nucleus_main(struct boot_info *bi) {
    g_com1 = COM1_PORT;
    serial_init(g_com1);
    kprintf_set_sink(serial_sink(&g_com1));
    kprintf("[nucleus] hello from the higher half (main=%p, boot_info=%p)\n",
            (void *)nucleus_main, (void *)bi);

    check_boot_info(bi);

    kprintf("BURROW:NUCLEUS:OK\n");
    qemu_exit(QEMU_EXIT_SUCCESS);
    cpu_halt_forever();
}
