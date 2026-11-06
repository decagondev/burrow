/* ktest_cputime.c - CPU time (W23-S02) through the nucleus calls Go uses:
 * a kernel thread that computes gains kernel time and one that sleeps
 * does not; a thread spinning in ring 3 gains user time, little kernel
 * time; every online CPU reports its user, kernel and idle time, which
 * together keep up with the clock. Never leaves a user thread behind. */
#include "ktest.h"
#include "ktest_nu.h"
#include "sched.h"
#include "smp.h"
#include "time.h"
#include "cputime.h"
#include "cpu.h"

#define MS (1000ull * 1000)

static void times(long tid, uint64_t t[2]) { nu(0x1029, tid, (long)t, 0, 0, 0); }

static uint64_t cpus_total(uint64_t *switches) {
    struct cputime_cpu c[16];
    long n = nu(0x102A, (long)c, 16, 0, 0, 0);
    uint64_t sum = 0;
    *switches = 0;
    for (long i = 0; i < n; i++) {
        sum += c[i].user_ns + c[i].sys_ns + c[i].idle_ns;
        *switches += c[i].switches;
    }
    return n == (long)smp_cpu_count() ? sum : 0;
}

static int check_times(void);

/* Ticks charge a running thread, so IF is on for the test (and restored). */
int kt_cputime(void) {
    uint64_t flags = read_rflags();
    cpu_sti();
    int r = check_times();
    irq_restore(flags);
    return r;
}

static int check_times(void) {
    long me = thread_id(thread_current());
    uint64_t a[2], b[2], c[2];
    times(me, a);
    time_busy_wait_ns(20 * MS);              /* computing in the kernel, ticks arriving */
    times(me, b);
    KT_CHECK(b[0] == a[0] && b[1] - a[1] >= 15 * MS);
    thread_sleep_ns(20 * MS);                /* not running: no time */
    times(me, c);
    KT_CHECK(c[1] - b[1] < 5 * MS);
    KT_CHECK(nu(0x1029, 0x7FFFFFFF, (long)c, 0, 0, 0) < 0);   /* no such thread */

    static const uint8_t spin[] = { 0xEB, 0xFE };             /* jmp $ */
    struct uproc p;
    KT_CHECK(uproc_new(&p, spin, sizeof spin, CODE_VA) == 0);
    long tid = nu(0x1020, p.as, CODE_VA, STACK_TOP, 0, 0);
    KT_CHECK(tid > 0);
    uint64_t s0, s1, t0 = cpus_total(&s0);
    thread_sleep_ns(40 * MS);                                 /* it spins meanwhile */
    uint64_t t1 = cpus_total(&s1), u[2];
    times(tid, u);
    KT_CHECK(nu(0x1023, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(uproc_free(&p) == 0);
    KT_CHECK(u[0] >= 20 * MS && u[1] < u[0] / 4);
    KT_CHECK(t0 > 0 && t1 - t0 >= 30 * MS && s1 > s0);
    return 0;
}
