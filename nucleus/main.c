/*
 * main.c - Burrow nucleus entry point: brings up each module in order.
 *
 * Runs at 0xFFFFFFFF80100000 on page tables the loader built. Each init step
 * lives in its own module and prints one marker (tests/smoke/expect/). Wave 6
 * adds "load and start the Go kernel" as the last step.
 *
 * Must never return.
 */
#include <stdint.h>
#include <burrow/boot_info.h>
#include "io.h"
#include "cpu.h"
#include "serial.h"
#include "kprintf.h"
#include "panic.h"
#include "gdt.h"
#include "trap.h"

#define NUCLEUS_VIRT_BASE 0xFFFFFFFF80000000ull   /* nucleus/linker.ld KERNEL_VMA */

static uint16_t g_com1;   /* serial_sink keeps a pointer to it */

void __attribute__((noreturn)) nucleus_main(struct boot_info *bi);

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

static void init_gdt(void) {
    gdt_init();
    if (read_cs() != SEL_KCODE || read_tr() != SEL_TSS) panic("GDT reload did not take");
    kprintf("[nucleus] gdt ok (cs=%x, tr=%x)\n", read_cs(), read_tr());
}

/* Prove the whole trap path (stub, IST, copy to kstack, dispatch, fixup, iretq)
 * with two faults we expect: #UD from ud2 and #GP from a non-canonical read. */
static void init_idt(void) {
    trap_init();
    uint64_t v;
    if (probe_ud2() != -1 || trap_last.vector != VEC_UD) panic("ud2 not caught");
    if (probe_read64(0x0000800000000000ull, &v) != -1 || trap_last.vector != VEC_GP)
        panic("non-canonical read not caught");
    kprintf("[nucleus] idt ok (fixups caught %s and %s)\n", trap_name(VEC_UD), trap_name(VEC_GP));
}

void __attribute__((noreturn)) nucleus_main(struct boot_info *bi) {
    g_com1 = COM1_PORT;
    serial_init(g_com1);
    kprintf_set_sink(serial_sink(&g_com1));
    kprintf("[nucleus] hello from the higher half (main=%p, boot_info=%p)\n",
            (void *)nucleus_main, (void *)bi);

    check_boot_info(bi);
    init_gdt();
    init_idt();

    kprintf("BURROW:NUCLEUS:OK\n");
    qemu_exit(QEMU_EXIT_SUCCESS);
    cpu_halt_forever();
}
