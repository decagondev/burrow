/* shim_thread.c - threads, sleeping and futexes for the Go runtime's Ms:
 * every M is a nucleus thread, every runtime lock and note a futex. */
#include "shim.h"
#include "sched.h"
#include "errno.h"

/* struct timespec {sec, nsec} -> ns; a negative or absurd value -> -1. */
static int64_t timespec_ns(const int64_t *ts) {
    if (ts[0] < 0 || ts[1] < 0 || ts[1] >= 1000000000ll) return -1;
    return ts[0] * 1000000000ll + ts[1];
}

long sys_sched_yield(struct syscall_frame *f) { (void)f; thread_yield(); return 0; }

long sys_nanosleep(struct syscall_frame *f) {
    int64_t ns = timespec_ns((const int64_t *)SYSARG0(f));
    if (ns < 0) return -EINVAL;
    thread_sleep_ns((uint64_t)ns);
    return 0;
}
