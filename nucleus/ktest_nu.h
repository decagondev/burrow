/* ktest_nu.h - ktests call the nucleus the way the Go kernel does: SYSCALL
 * with the Linux register convention (NUCLEUS_ABI.md). */
#ifndef NUCLEUS_KTEST_NU_H
#define NUCLEUS_KTEST_NU_H
#include <stdint.h>

static inline long nu(long nr, long a0, long a1, long a2, long a3, long a4) {
    long ret;
    register long r10 __asm__("r10") = a3;
    register long r8 __asm__("r8") = a4;
    __asm__ volatile("syscall" : "=a"(ret) : "a"(nr), "D"(a0), "S"(a1), "d"(a2), "r"(r10), "r"(r8)
                     : "rcx", "r11", "memory");
    return ret;
}
/* Tiny processes for the ring-3 ktests (ktest_uthread.c): an address space
 * with code at a page and a stack page; requests land in g_req. */
#define CODE_VA    0x400000ull
#define STACK_VA   0x7FFF0000ull
#define STACK_TOP  (STACK_VA + 0x1000)
#define FAULT(v)   (0xFFFF0000ull | (v))

struct uproc { long as, code, stack; };
int  uproc_new(struct uproc *p, const uint8_t *code, uint64_t len, uint64_t va);
int  uproc_free(struct uproc *p);
extern uint64_t g_req[];
long next_req(void);
#endif
