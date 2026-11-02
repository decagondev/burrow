#include "serial.h"
#include "io.h"

enum { UART_DATA = 0, UART_IER = 1, UART_FCR = 2, UART_LCR = 3, UART_MCR = 4, UART_LSR = 5 };
#define LSR_THR_EMPTY 0x20

void serial_init(uint16_t base) {
    outb(base + UART_IER, 0x00);   /* no interrupts                */
    outb(base + UART_LCR, 0x80);   /* DLAB on                      */
    outb(base + UART_DATA, 0x01);  /* divisor 1 = 115200 baud      */
    outb(base + UART_IER, 0x00);
    outb(base + UART_LCR, 0x03);   /* 8N1, DLAB off                */
    outb(base + UART_FCR, 0xC7);   /* FIFO on, cleared             */
    outb(base + UART_MCR, 0x03);   /* DTR + RTS                    */
}

void serial_putc(uint16_t base, char c) {
    if (c == '\n') serial_putc(base, '\r');        /* terminals want CRLF */
    while (!(inb(base + UART_LSR) & LSR_THR_EMPTY)) { }
    outb(base + UART_DATA, (uint8_t)c);
}

static void sink_putc(void *ctx, char c) { serial_putc(*(uint16_t *)ctx, c); }

struct char_sink serial_sink(uint16_t *base) {
    struct char_sink s = { sink_putc, base };
    return s;
}
