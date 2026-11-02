/* panic.h - stop the machine loudly: one "[nucleus] PANIC: ..." line on the
 * console, QEMU debug-exit failure (status 35 in tests), halt. */
#ifndef NUCLEUS_PANIC_H
#define NUCLEUS_PANIC_H

void panic(const char *fmt, ...) __attribute__((noreturn, format(printf, 1, 2)));
#endif
