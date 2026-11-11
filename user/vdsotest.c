/* vdsotest.c - the vDSO answers like the system calls (W25-S02), on the
 * host's Linux and on Burrow alike: AT_SYSINFO_EHDR names it; each clock
 * read through it (musl's clock_gettime) lies between two system-call
 * readings of the same clock; gettimeofday and time agree with theirs;
 * getcpu gives a CPU that exists; four threads reading the monotonic clock
 * never see it step back, whichever CPUs they run on; and how long a read
 * takes. Prints "vdsotest: <check> ok" lines and "vdsotest: ok", or
 * "vdsotest: FAIL ..." and exits 1. Never changes a clock.
 * Docs: docs/04-plan/waves/WAVE-25-speed.md. */
#define _GNU_SOURCE
#include <pthread.h>
#include <sched.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/auxv.h>
#include <sys/syscall.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>

#define THREADS      4
#define READS        20000    /* per thread, across CPUs */
#define TIMED_READS  100000
#define NS           1000000000LL

static int failed;
#define FAIL(...) do { printf("vdsotest: FAIL "); printf(__VA_ARGS__); printf("\n"); failed = 1; } while (0)

static long long ns_of(const struct timespec *ts) { return ts->tv_sec * NS + ts->tv_nsec; }

static long long sys_clock(clockid_t c) {
    struct timespec ts;
    if (syscall(SYS_clock_gettime, c, &ts)) return -1;
    return ns_of(&ts);
}

static void check_clock(const char *name, clockid_t c) {
    struct timespec ts;
    long long a = sys_clock(c);
    if (clock_gettime(c, &ts)) { FAIL("%s: clock_gettime failed", name); return; }
    long long b = ns_of(&ts), z = sys_clock(c);
    if (a < 0 || z < 0) { FAIL("%s: the system call failed", name); return; }
    if (b < a || b > z) FAIL("%s: %lld not between the system call's %lld and %lld", name, b, a, z);
    else printf("vdsotest: %s ok\n", name);
}

static atomic_llong last;   /* the latest monotonic reading any thread made */
static void *reader(void *arg) {
    (void)arg;
    for (int i = 0; i < READS; i++) {
        long long seen = atomic_load(&last);
        struct timespec ts;
        clock_gettime(CLOCK_MONOTONIC, &ts);
        long long now = ns_of(&ts);
        if (now < seen) { FAIL("monotonic stepped back %lld ns on CPU %d", seen - now, sched_getcpu()); return 0; }
        while (seen < now && !atomic_compare_exchange_weak(&last, &seen, now)) {}
    }
    return 0;
}

int main(void) {
    setvbuf(stdout, 0, _IOLBF, 0);
    unsigned long ehdr = getauxval(AT_SYSINFO_EHDR);
    if (!ehdr) FAIL("no AT_SYSINFO_EHDR");
    else printf("vdsotest: vdso at 0x%lx\n", ehdr);

    check_clock("realtime", CLOCK_REALTIME);
    check_clock("monotonic", CLOCK_MONOTONIC);
    check_clock("boottime", CLOCK_BOOTTIME);

    struct timeval tv;
    long long before = sys_clock(CLOCK_REALTIME) / 1000;
    gettimeofday(&tv, 0);
    long long us = tv.tv_sec * 1000000LL + tv.tv_usec, after = sys_clock(CLOCK_REALTIME) / 1000;
    if (us < before || us > after) FAIL("gettimeofday %lld not between %lld and %lld", us, before, after);
    else printf("vdsotest: gettimeofday ok\n");

    long t = time(0), st = syscall(SYS_time, 0);
    if (t < st - 1 || t > st + 1) FAIL("time %ld, the system call %ld", t, st);
    else printf("vdsotest: time ok\n");

    int cpu = sched_getcpu(), ncpu = (int)sysconf(_SC_NPROCESSORS_ONLN);
    if (cpu < 0 || cpu >= ncpu) FAIL("getcpu %d of %d CPUs", cpu, ncpu);
    else printf("vdsotest: getcpu ok\n");

    pthread_t th[THREADS];
    for (int i = 0; i < THREADS; i++) pthread_create(&th[i], 0, reader, 0);
    for (int i = 0; i < THREADS; i++) pthread_join(th[i], 0);
    if (!failed) printf("vdsotest: monotonic across threads ok\n");

    struct timespec ts, t0, t1;
    clock_gettime(CLOCK_MONOTONIC, &t0);
    for (int i = 0; i < TIMED_READS; i++) clock_gettime(CLOCK_MONOTONIC, &ts);
    clock_gettime(CLOCK_MONOTONIC, &t1);
    printf("vdsotest: a clock read takes %lld ns\n", (ns_of(&t1) - ns_of(&t0)) / TIMED_READS);

    if (failed) return 1;
    printf("vdsotest: ok\n");
    return 0;
}
