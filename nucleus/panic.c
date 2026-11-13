#include <stdarg.h>
#include "panic.h"
#include "kprintf.h"
#include "io.h"
#include "cpu.h"
#include "screen.h"

void panic(const char *fmt, ...) {
    cpu_cli();
    screen_panic();   /* W26-S04: on the screen too, when the firmware gave one */
    kprintf("[nucleus] PANIC: ");
    va_list ap;
    va_start(ap, fmt);
    kvprintf(fmt, ap);
    va_end(ap);
    kprintf("\n");
    qemu_exit(QEMU_EXIT_FAILURE);
    cpu_halt_forever();
}
