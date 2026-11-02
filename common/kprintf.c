/* kprintf.c - formatting is pure logic: it only talks to a char_sink.
 * That is why tests/unit/c/test_kprintf.c can test it on the host. */
#include <stdint.h>
#include "kprintf.h"

static struct char_sink g_sink;   /* zero => output discarded until set */

void kprintf_set_sink(struct char_sink sink) { g_sink = sink; }

struct out { struct char_sink sink; int count; };

static void emit(struct out *o, char c) {
    if (o->sink.putc) o->sink.putc(o->sink.ctx, c);
    o->count++;
}

static void emit_uint(struct out *o, unsigned long long v, unsigned base,
                      int upper, int width, char pad) {
    const char *digits = upper ? "0123456789ABCDEF" : "0123456789abcdef";
    char buf[32];
    int n = 0;
    do { buf[n++] = digits[v % base]; v /= base; } while (v);
    while (n < width && n < (int)sizeof buf) buf[n++] = pad;
    while (n) emit(o, buf[--n]);
}

int kvprintf_to(struct char_sink sink, const char *fmt, va_list ap) {
    struct out o = { sink, 0 };
    for (; *fmt; fmt++) {
        if (*fmt != '%') { emit(&o, *fmt); continue; }
        fmt++;
        char pad = ' ';
        int width = 0, lng = 0;
        if (*fmt == '0') { pad = '0'; fmt++; }
        while (*fmt >= '0' && *fmt <= '9') width = width * 10 + (*fmt++ - '0');
        while (*fmt == 'l' || *fmt == 'z') { lng++; fmt++; }

        switch (*fmt) {
        case '%': emit(&o, '%'); break;
        case 'c': emit(&o, (char)va_arg(ap, int)); break;
        case 's': {
            const char *s = va_arg(ap, const char *);
            if (!s) s = "(null)";
            while (*s) emit(&o, *s++);
            break;
        }
        case 'd': case 'i': {
            long long v = lng ? va_arg(ap, long long) : va_arg(ap, int);
            unsigned long long mag;
            if (v < 0) { emit(&o, '-'); mag = 0ull - (unsigned long long)v; if (width) width--; }
            else mag = (unsigned long long)v;
            emit_uint(&o, mag, 10, 0, width, pad);
            break;
        }
        case 'u': case 'x': case 'X': {
            unsigned long long v = lng ? va_arg(ap, unsigned long long) : va_arg(ap, unsigned);
            emit_uint(&o, v, *fmt == 'u' ? 10 : 16, *fmt == 'X', width, pad);
            break;
        }
        case 'p':
            emit(&o, '0'); emit(&o, 'x');
            emit_uint(&o, (uintptr_t)va_arg(ap, void *), 16, 0, 16, '0');
            break;
        case '\0': return o.count;          /* stray '%' at end of string */
        default:   emit(&o, '%'); emit(&o, *fmt); break;  /* unknown: echo it */
        }
    }
    return o.count;
}

int kprintf(const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    int n = kvprintf_to(g_sink, fmt, ap);
    va_end(ap);
    return n;
}

int kvprintf(const char *fmt, va_list ap) { return kvprintf_to(g_sink, fmt, ap); }
