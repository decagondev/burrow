/* shim_proc.c - the "boring" part of the Linux subset: who am I, signals
 * (accepted, never delivered: no signals in ring 0, PIT-06), the three
 * console fds, limits, CPU count, TLS, time, randomness. Pointer arguments
 * come from the Go kernel, which shares our address space and is trusted. */
#include "shim.h"
#include "sched.h"
#include "time.h"
#include "errno.h"
#include "cpu.h"
#include "serial.h"
#include "spinlock.h"
#include "kprintf.h"
#include "lib_string.h"
#include "panic.h"

#define ARCH_SET_FS   0x1002
#define ARCH_GET_FS   0x1003
#define SS_DISABLE    2
#define F_GETFD       1
#define F_GETFL       3
#define O_RDWR        2

long sys_getpid(struct syscall_frame *f) { (void)f; return 1; }
long sys_gettid(struct syscall_frame *f) { (void)f; return thread_id(thread_current()); }

/* fds 0-2 are the console (Go >= 1.22 checks them at start-up). */
long sys_write(struct syscall_frame *f) {
    if (SYSARG0(f) > 2) return poll_write((int)SYSARG0(f), (const void *)SYSARG1(f), SYSARG2(f));
    if (SYSARG0(f) == 0) return -EBADF;
    const char *buf = (const char *)SYSARG1(f);
    uint64_t n = SYSARG2(f), flags = kprint_enter();   /* not inside another CPU's kprintf line */
    for (uint64_t i = 0; i < n; i++) serial_putc(COM1_PORT, buf[i]);
    kprint_leave(flags);
    return (long)n;
}
long sys_read(struct syscall_frame *f) {
    if (SYSARG0(f) > 2) return poll_read((int)SYSARG0(f), (void *)SYSARG1(f), SYSARG2(f));
    return SYSARG0(f) == 0 ? 0 : -EBADF;                  /* stdin: EOF */
}
/* Only reached after an openat succeeds, which it never does yet (no VFS). */
long sys_pread64(struct syscall_frame *f) { (void)f; return -EBADF; }
long sys_close(struct syscall_frame *f) { return SYSARG0(f) > 2 ? poll_close((int)SYSARG0(f)) : 0; }

/* Old action: "default, no flags" (32 bytes: handler, flags, restorer, mask). */
long sys_rt_sigaction(struct syscall_frame *f) {
    if (SYSARG2(f)) memset((void *)SYSARG2(f), 0, 32);
    return 0;
}
long sys_rt_sigprocmask(struct syscall_frame *f) {
    if (SYSARG2(f)) memset((void *)SYSARG2(f), 0, 8);
    return 0;
}
/* Old stack: none (stack_t: sp, flags, size). */
long sys_sigaltstack(struct syscall_frame *f) {
    if (SYSARG1(f)) {
        uint64_t *old = (uint64_t *)SYSARG1(f);
        old[0] = 0; old[1] = SS_DISABLE; old[2] = 0;
    }
    return 0;
}

long sys_fcntl(struct syscall_frame *f) {
    if (SYSARG0(f) > 2) return -EBADF;
    if (SYSARG1(f) == F_GETFD) return 0;
    if (SYSARG1(f) == F_GETFL) return O_RDWR;
    return 0;
}
long sys_openat(struct syscall_frame *f) { (void)f; return -ENOENT; }   /* no files until W8 */

static void fill_rlimit(uint64_t *rl) { rl[0] = rl[1] = 1024; }
long sys_getrlimit(struct syscall_frame *f) {
    if (!SYSARG1(f)) return -EFAULT;
    fill_rlimit((uint64_t *)SYSARG1(f));
    return 0;
}
/* prlimit64(pid, resource, new, old): report 1024 fds, accept any new limit. */
long sys_prlimit64(struct syscall_frame *f) {
    if (SYSARG3(f)) fill_rlimit((uint64_t *)SYSARG3(f));
    return 0;
}

long sys_sched_getaffinity(struct syscall_frame *f) {
    if (SYSARG1(f) < 8) return -EINVAL;
    uint8_t *mask = (uint8_t *)SYSARG2(f);
    memset(mask, 0, 8);
    mask[0] = 1;                                   /* one CPU until an SMP wave */
    return 8;                                      /* bytes written */
}

/* The runtime keeps its g pointer at -8(FS); GS stays the nucleus's (PIT-04). */
long sys_arch_prctl(struct syscall_frame *f) {
    if (SYSARG0(f) == ARCH_SET_FS) { thread_set_fs_base(thread_current(), SYSARG1(f)); return 0; }
    if (SYSARG0(f) == ARCH_GET_FS) { *(uint64_t *)SYSARG1(f) = thread_current()->fs_base; return 0; }
    return -EINVAL;
}

/* The realtime clocks (REALTIME, REALTIME_COARSE, REALTIME_ALARM, TAI) are
 * wall time once Go has set it from the CMOS clock (W14-S04); every other
 * clock is the monotonic one. */
#define CLOCK_REALTIME_MASK ((1u << 0) | (1u << 5) | (1u << 8) | (1u << 11))
long sys_clock_gettime(struct syscall_frame *f) {
    uint64_t id = SYSARG0(f);
    uint64_t ns = id < 32 && (CLOCK_REALTIME_MASK >> id & 1) ? time_realtime_ns() : time_monotonic_ns();
    int64_t *ts = (int64_t *)SYSARG1(f);
    ts[0] = (int64_t)(ns / 1000000000ull);
    ts[1] = (int64_t)(ns % 1000000000ull);
    return 0;
}

/* Not cryptographic: TSC jitter through a 64-bit mixer. Fine for map hash seeds. */
long sys_getrandom(struct syscall_frame *f) {
    uint8_t *buf = (uint8_t *)SYSARG0(f);
    uint64_t n = SYSARG1(f), x = rdtsc();
    for (uint64_t i = 0; i < n; i++) {
        if (i % 8 == 0) { x ^= rdtsc() + i; x *= 0x9E3779B97F4A7C15ull; x ^= x >> 31; }
        buf[i] = (uint8_t)(x >> (8 * (i % 8)));
    }
    return (long)n;
}

/* The runtime exits after a Go panic or fatal error (status 2), once it has
 * written the message and every goroutine's stack to fd 2: fail the run now. */
long sys_exit_group(struct syscall_frame *f) {
    panic("the Go kernel exited with status %lld (a Go panic or fatal error: see above)",
          (long long)SYSARG0(f));
}
