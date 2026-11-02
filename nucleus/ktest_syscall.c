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

/* "Census on failure": an unknown number is logged once and returns -ENOSYS. */
int kt_shim_unknown_once(void) {
    uint64_t before = shim_unknown_reports();
    KT_CHECK(sys3(999, 1, 2, 3) == -ENOSYS);
    KT_CHECK(sys3(999, 1, 2, 3) == -ENOSYS);
    KT_CHECK(shim_unknown_reports() == before + 1);
    return 0;
}
