/* cpu.h - CPUID, MSRs and control registers. With io.h, the only shared
 * header allowed inline asm. Never included by host-tested modules. */
#ifndef COMMON_CPU_H
#define COMMON_CPU_H
#include <stdint.h>

#define MSR_EFER  0xC0000080u
#define EFER_NXE  (1ull << 11)      /* allow bit 63 (NX) in page-table entries */

static inline void cpuid(uint32_t leaf, uint32_t *a, uint32_t *b, uint32_t *c, uint32_t *d) {
    __asm__ volatile("cpuid" : "=a"(*a), "=b"(*b), "=c"(*c), "=d"(*d) : "a"(leaf), "c"(0));
}
static inline uint64_t rdmsr(uint32_t msr) {
    uint32_t lo, hi;
    __asm__ volatile("rdmsr" : "=a"(lo), "=d"(hi) : "c"(msr));
    return ((uint64_t)hi << 32) | lo;
}
static inline void wrmsr(uint32_t msr, uint64_t v) {
    __asm__ volatile("wrmsr" :: "c"(msr), "a"((uint32_t)v), "d"((uint32_t)(v >> 32)));
}
/* CPUID 0x80000001 EDX bit 20: execute-disable. */
static inline int cpu_has_nx(void) {
    uint32_t a, b, c, d;
    cpuid(0x80000000u, &a, &b, &c, &d);
    if (a < 0x80000001u) return 0;
    cpuid(0x80000001u, &a, &b, &c, &d);
    return (d >> 20) & 1;
}
#endif
