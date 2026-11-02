/*
 * main.c - Burrow nucleus entry point (Wave 4 scope: "hello from the higher half").
 *
 * Runs at 0xFFFFFFFF80100000 on page tables the loader built. Today it only
 * proves the hand-off: it prints where it runs and stops. Wave 5 grows it into
 * the real nucleus (GDT/TSS/IDT, PMM, VMM, threads) by calling NEW modules.
 *
 * Shares the freestanding serial/kprintf/string code with the loader (built
 * twice, with -mcmodel=kernel here). Must never return.
 */
#include <stdint.h>
#include <burrow/boot_info.h>
#include "io.h"
#include "serial.h"
#include "kprintf.h"

static uint16_t g_com1;   /* serial_sink keeps a pointer to it */

void __attribute__((noreturn)) nucleus_main(struct boot_info *bi) {
    g_com1 = COM1_PORT;
    serial_init(g_com1);
    kprintf_set_sink(serial_sink(&g_com1));
    kprintf("[nucleus] hello from the higher half (main=%p, boot_info=%p)\n",
            (void *)nucleus_main, (void *)bi);

    kprintf("BURROW:NUCLEUS:OK\n");
    qemu_exit(QEMU_EXIT_SUCCESS);
    cpu_halt_forever();
}
