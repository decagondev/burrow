/*
 * main.c - Burrow nucleus entry point: brings up each module in order.
 *
 * Runs at 0xFFFFFFFF80100000 on page tables the loader built. Each init step
 * lives in its own module and prints one marker (tests/smoke/expect/). If a
 * "ktest" boot module is present, the in-kernel tests run instead of the
 * normal finish; then the Go kernel starts (W6) if the disk has one.
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
#include "boot.h"
#include "gdt.h"
#include "trap.h"
#include "frames.h"
#include "vmm.h"
#include "apic.h"
#include "time.h"
#include "fpu.h"
#include "sched.h"
#include "syscall.h"
#include "irq.h"
#include "go_load.h"
#include "ktest.h"
#include "protect.h"

static uint16_t g_com1;   /* serial_sink keeps a pointer to it */

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
    kprintf_set_guard(irq_save, irq_restore);   /* one line at a time across threads */
    kprintf("[nucleus] hello from the higher half (main=%p, boot_info=%p)\n",
            (void *)nucleus_main, (void *)bi);

    boot_init(bi);
    init_gdt();
    init_idt();
    frames_init();
    vmm_init();
    protect_init();
    pic_disable();
    lapic_init();
    time_init();
    fpu_init();
    sched_init();
    syscall_init();
    irq_init();

    const struct boot_module *kt = boot_module_find("ktest");
    if (kt) {
        kprintf("[ktest] running suite (%llu bytes)\n", (unsigned long long)kt->size_bytes);
        ktest_run(phys_to_virt(kt->phys_start), kt->size_bytes);
    }

    kprintf("BURROW:NUCLEUS:OK\n");
    if (go_kernel_start() == 0) {
        kprintf("[nucleus] starting the Go kernel\n");
        thread_exit();                 /* "main" is done; Go threads take over */
    }
    qemu_exit(QEMU_EXIT_SUCCESS);      /* no Go kernel on this disk: nucleus-only boot */
    cpu_halt_forever();
}
