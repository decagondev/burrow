/* io.h - x86 port I/O. With cpu.h, the only shared header allowed inline asm. */
#ifndef COMMON_IO_H
#define COMMON_IO_H
#include <stdint.h>

static inline void outb(uint16_t port, uint8_t v) { __asm__ volatile("outb %0, %1" :: "a"(v), "Nd"(port)); }
static inline uint8_t inb(uint16_t port) { uint8_t v; __asm__ volatile("inb %1, %0" : "=a"(v) : "Nd"(port)); return v; }
static inline void outw(uint16_t port, uint16_t v) { __asm__ volatile("outw %0, %1" :: "a"(v), "Nd"(port)); }
static inline uint16_t inw(uint16_t port) { uint16_t v; __asm__ volatile("inw %1, %0" : "=a"(v) : "Nd"(port)); return v; }
static inline void io_wait(void) { outb(0x80, 0); } /* ~1us: write to unused POST port */
static inline __attribute__((noreturn)) void cpu_halt_forever(void) { for (;;) __asm__ volatile("cli; hlt"); }

/* QEMU isa-debug-exit: QEMU exits with status (code << 1) | 1.
 * On real hardware port 0xF4 is unused, so this is a harmless no-op. */
#define QEMU_EXIT_SUCCESS 0x10   /* -> process exit status 33 */
#define QEMU_EXIT_FAILURE 0x11   /* -> process exit status 35 */
static inline void qemu_exit(uint8_t code) { outb(0xF4, code); }
#endif
