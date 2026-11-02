/* ktest_syscall.c - the SYSCALL gate and the shim, called from nucleus code
 * (ring 0, like the Go kernel). */
#include "ktest.h"
#include "syscall.h"
#include "errno.h"
#include "cpu.h"

extern uint64_t ktest_syscall_clobbers(void);
extern uint64_t shim_unknown_reports(void);

static long sys3(long nr, long a0, long a1, long a2) {
    long ret;
    register long r10 __asm__("r10") = 0;
    __asm__ volatile("syscall" : "=a"(ret) : "a"(nr), "D"(a0), "S"(a1), "d"(a2), "r"(r10)
                     : "rcx", "r11", "memory");
    return ret;
}

int kt_syscall_getpid(void) {
    uint64_t if_before = read_rflags() & RFLAGS_IF;
    KT_CHECK(sys3(39, 0, 0, 0) == 1);
    KT_CHECK((read_rflags() & RFLAGS_IF) == if_before);    /* IRETQ restored IF */
    KT_CHECK(ktest_syscall_clobbers() == 0);                /* only RAX, RCX, R11 change */
    return 0;
}

int kt_syscall_write(void) {
    static const char msg[] = "[ktest] hello through write(1)\n";
    KT_CHECK(sys3(1, 1, (long)msg, sizeof msg - 1) == (long)(sizeof msg - 1));
    KT_CHECK(sys3(1, 7, (long)msg, 1) == -EBADF);
    return 0;
}

static long sys6(long nr, long a0, long a1, long a2, long a3, long a4, long a5) {
    long ret;
    register long r10 __asm__("r10") = a3;
    register long r8 __asm__("r8") = a4;
    register long r9 __asm__("r9") = a5;
    __asm__ volatile("syscall" : "=a"(ret) : "a"(nr), "D"(a0), "S"(a1), "d"(a2), "r"(r10), "r"(r8), "r"(r9)
                     : "rcx", "r11", "memory");
    return ret;
}

/* BUG-0009: after madvise(MADV_DONTNEED) anonymous memory must read as zeros
 * (Go 1.27 hands out scavenged spans without clearing them). */
int kt_madvise_dontneed_zeroes(void) {
    long addr = sys6(9, 0, 3 * 4096, 3 /* RW */, 0x22 /* PRIVATE|ANON */, -1, 0);
    KT_CHECK(addr > 0);
    uint64_t *p = (uint64_t *)addr;
    p[0] = 0x1111; p[512] = 0x2222; p[1024] = 0x3333;          /* one word per page */
    KT_CHECK(sys3(28, addr + 4096, 4096, 4 /* MADV_DONTNEED */) == 0);
    KT_CHECK(p[0] == 0x1111 && p[512] == 0 && p[1024] == 0x3333);   /* only the middle page */
    p[512] = 0x4444;                                           /* still mapped and writable */
    KT_CHECK(sys3(28, addr, 3 * 4096, 15 /* MADV_NOHUGEPAGE: a hint */) == 0);
    KT_CHECK(p[0] == 0x1111 && p[512] == 0x4444);
    KT_CHECK(sys3(11, addr, 3 * 4096, 0) == 0);                 /* munmap */
    return 0;
}

/* "Census on failure": an unknown number is logged once and returns -ENOSYS. */
int kt_shim_unknown_once(void) {
    uint64_t before = shim_unknown_reports();
    KT_CHECK(sys3(999, 1, 2, 3) == -ENOSYS);
    KT_CHECK(sys3(999, 1, 2, 3) == -ENOSYS);
    KT_CHECK(shim_unknown_reports() == before + 1);
    return 0;
}
