#include <stdarg.h>
#include "minitest.h"
#include "kprintf.h"

/* A char_sink that writes into a buffer: the test double for the UART. */
struct buf { char data[256]; int len; };
static void buf_putc(void *ctx, char c) {
    struct buf *b = ctx;
    if (b->len < (int)sizeof b->data - 1) b->data[b->len++] = c;
    b->data[b->len] = 0;
}
static const char *fmt(const char *f, ...) {
    static struct buf b;
    b.len = 0; b.data[0] = 0;
    struct char_sink s = { buf_putc, &b };
    va_list ap; va_start(ap, f); kvprintf_to(s, f, ap); va_end(ap);
    return b.data;
}

TEST(kprintf_plain)    { CHECK_STR(fmt("hello"), "hello"); }
TEST(kprintf_decimal)  { CHECK_STR(fmt("%d %i %u", -42, 7, 3000000000u), "-42 7 3000000000"); }
TEST(kprintf_hex)      { CHECK_STR(fmt("%x %X %08x", 0xbeef, 0xbeef, 0x1f), "beef BEEF 0000001f"); }
TEST(kprintf_long)     { CHECK_STR(fmt("%llx", 0x123456789abcdefull), "123456789abcdef"); }
TEST(kprintf_string)   { CHECK_STR(fmt("[%s|%s]", "a", (char *)0), "[a|(null)]"); }
TEST(kprintf_char_pct) { CHECK_STR(fmt("%c%%", 'Z'), "Z%"); }
TEST(kprintf_pointer)  { CHECK_STR(fmt("%p", (void *)0x10000), "0x0000000000010000"); }
TEST(kprintf_unknown)  { CHECK_STR(fmt("%q"), "%q"); }
TEST(kprintf_int_min)  { CHECK_STR(fmt("%lld", (long long)(-9223372036854775807LL - 1)), "-9223372036854775808"); }
TEST(kprintf_no_sink_is_safe) {
    struct char_sink none = { 0, 0 };
    va_list *unused = 0; (void)unused;
    kprintf_set_sink(none);
    CHECK(kprintf("discarded %d", 1) == 11);   /* still counts characters */
}
