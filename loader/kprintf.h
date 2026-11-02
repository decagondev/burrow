/* kprintf.h - tiny printf. Supports: %% %c %s %d %i %u %x %X %p
 * with optional '0' flag, width, and 'l' / 'll' / 'z' length modifiers. */
#ifndef LOADER_KPRINTF_H
#define LOADER_KPRINTF_H
#include <stdarg.h>
#include "char_sink.h"

void kprintf_set_sink(struct char_sink sink);              /* where kprintf goes */
int  kprintf(const char *fmt, ...) __attribute__((format(printf, 1, 2)));
int  kvprintf_to(struct char_sink sink, const char *fmt, va_list ap); /* pure, testable */
#endif
