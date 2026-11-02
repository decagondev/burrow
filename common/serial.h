/* serial.h - 16550 UART driver (polled). Exposes itself as a char_sink. */
#ifndef COMMON_SERIAL_H
#define COMMON_SERIAL_H
#include <stdint.h>
#include "char_sink.h"

#define COM1_PORT 0x3F8

void serial_init(uint16_t base);
void serial_putc(uint16_t base, char c);
struct char_sink serial_sink(uint16_t *base);   /* adapter: UART -> char_sink */
#endif
