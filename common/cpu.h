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
static inline uint64_t read_cr0(void) { uint64_t v; __asm__ volatile("mov %%cr0, %0" : "=r"(v)); return v; }
static inline uint64_t read_cr2(void) { uint64_t v; __asm__ volatile("mov %%cr2, %0" : "=r"(v)); return v; }
static inline uint64_t read_cr3(void) { uint64_t v; __asm__ volatile("mov %%cr3, %0" : "=r"(v)); return v; }
static inline uint64_t read_cr4(void) { uint64_t v; __asm__ volatile("mov %%cr4, %0" : "=r"(v)); return v; }
static inline void write_cr0(uint64_t v) { __asm__ volatile("mov %0, %%cr0" :: "r"(v) : "memory"); }
static inline void write_cr3(uint64_t v) { __asm__ volatile("mov %0, %%cr3" :: "r"(v) : "memory"); }
/* SMAP (W20-S02): stac lets ring 0 touch user pages until clac. */
static inline void stac(void) { __asm__ volatile("stac" ::: "memory"); }
static inline void clac(void) { __asm__ volatile("clac" ::: "memory"); }
static inline void write_cr4(uint64_t v) { __asm__ volatile("mov %0, %%cr4" :: "r"(v) : "memory"); }
static inline void invlpg(uint64_t va) { __asm__ volatile("invlpg (%0)" :: "r"(va) : "memory"); }
static inline uint16_t read_cs(void) { uint16_t v; __asm__ volatile("mov %%cs, %0" : "=r"(v)); return v; }
static inline uint16_t read_tr(void) { uint16_t v; __asm__ volatile("str %0" : "=r"(v)); return v; }
/* Per-CPU data (W20-S05): a qword at GS_BASE + off (nucleus/percpu.h). */
static inline uint64_t read_gs_qword(uint64_t off) {
    uint64_t v;
    __asm__ volatile("mov %%gs:(%1), %0" : "=r"(v) : "r"(off) : "memory");
    return v;
}
static inline void cpu_pause(void) { __asm__ volatile("pause" ::: "memory"); }
static inline uint64_t rdtsc(void) {
    uint32_t lo, hi;
    __asm__ volatile("rdtsc" : "=a"(lo), "=d"(hi));
    return ((uint64_t)hi << 32) | lo;
}
static inline uint64_t read_rflags(void) { uint64_t v; __asm__ volatile("pushfq; pop %0" : "=r"(v)); return v; }
static inline void cpu_cli(void) { __asm__ volatile("cli" ::: "memory"); }
static inline void cpu_sti(void) { __asm__ volatile("sti" ::: "memory"); }
/* sti then hlt: an interrupt pending at the sti ends the hlt (one-instruction shadow). */
static inline void cpu_sti_hlt(void) { __asm__ volatile("sti; hlt" ::: "memory"); }
#define RFLAGS_IF (1ull << 9)
/* Disable interrupts, return the previous RFLAGS for irq_restore. On one CPU
 * this alone makes a short section atomic with respect to IRQ handlers. */
static inline uint64_t irq_save(void) { uint64_t f = read_rflags(); cpu_cli(); return f; }
static inline void irq_restore(uint64_t flags) { if (flags & RFLAGS_IF) cpu_sti(); }

/* CPUID 0x80000001 EDX bit 20: execute-disable. */
static inline int cpu_has_nx(void) {
    uint32_t a, b, c, d;
    cpuid(0x80000000u, &a, &b, &c, &d);
    if (a < 0x80000001u) return 0;
    cpuid(0x80000001u, &a, &b, &c, &d);
    return (d >> 20) & 1;
}

/* The loaders' last step (loader/handoff.c): load cr3, jump to entry with
 * RDI = arg. The code doing it must be mapped the same in both address
 * spaces (the identity map, PIT-21); nothing touches the stack after cr3. */
static inline __attribute__((noreturn)) void cpu_enter_space(uint64_t cr3, uint64_t entry, uint64_t arg) {
    __asm__ volatile("mov %0, %%cr3\n\tjmp *%1" :: "r"(cr3), "r"(entry), "D"(arg) : "memory");
    __builtin_unreachable();
}
#endif
