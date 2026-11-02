/* char_sink.h - the smallest possible "output device" interface.
 *
 * Dependency Inversion in C: kprintf depends on this abstraction, not on the
 * UART. The serial driver, a framebuffer console, or a test buffer can all
 * provide one. Struct-of-function-pointers is C's version of a Go interface.
 */
#ifndef LOADER_CHAR_SINK_H
#define LOADER_CHAR_SINK_H

struct char_sink {
    void (*putc)(void *ctx, char c);
    void *ctx;
};

#endif
