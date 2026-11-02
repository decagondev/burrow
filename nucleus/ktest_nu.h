/* ktest_nu.h - ktests call the nucleus the way the Go kernel does: SYSCALL
 * with the Linux register convention (NUCLEUS_ABI.md). */
#ifndef NUCLEUS_KTEST_NU_H
#define NUCLEUS_KTEST_NU_H

static inline long nu(long nr, long a0, long a1, long a2, long a3, long a4) {
    long ret;
    register long r10 __asm__("r10") = a3;
    register long r8 __asm__("r8") = a4;
    __asm__ volatile("syscall" : "=a"(ret) : "a"(nr), "D"(a0), "S"(a1), "d"(a2), "r"(r10), "r"(r8)
                     : "rcx", "r11", "memory");
    return ret;
}
#endif
