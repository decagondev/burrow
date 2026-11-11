/* vdso.c - Burrow's vDSO (W25-S02): clock_gettime, gettimeofday, time and
 * getcpu answered in the calling process, from the data page the nucleus
 * keeps one page below this image (include/burrow/vdso.h) and the TSC, as
 * Linux's vDSO does; musl and Go find these by name and version
 * (LINUX_2.6, vdso.lds). Clocks the page cannot serve, and a page not yet
 * filled, go to the system call. Built freestanding and position
 * independent with no relocations (the Makefile checks), so the image
 * runs wherever the kernel maps it. Never writes the data page.
 * Docs: docs/04-plan/waves/WAVE-25-speed.md. */
#include <stdint.h>
#include "burrow/vdso.h"

#define CLOCK_REALTIME           0
#define CLOCK_MONOTONIC          1
#define CLOCK_MONOTONIC_RAW      4
#define CLOCK_REALTIME_COARSE    5
#define CLOCK_MONOTONIC_COARSE   6
#define CLOCK_BOOTTIME           7
#define SYS_gettimeofday         96
#define SYS_time                 201
#define SYS_clock_gettime        228
#define NS_PER_S                 1000000000ull

struct timespec { long tv_sec, tv_nsec; };
struct timeval { long tv_sec, tv_usec; };
struct timezone { int tz_minuteswest, tz_dsttime; };

/* The data page, one page below this image (vdso.lds). */
extern const volatile struct vdso_data vvar_page __attribute__((visibility("hidden")));

static inline uint64_t rdtsc(void) {
    uint32_t lo, hi;
    __asm__ volatile("rdtsc" : "=a"(lo), "=d"(hi));
    return ((uint64_t)hi << 32) | lo;
}

static long sys2(long nr, long a, long b) {
    long r;
    __asm__ volatile("syscall" : "=a"(r) : "a"(nr), "D"(a), "S"(b) : "rcx", "r11", "memory");
    return r;
}

/* The clock in ns (realtime or monotonic); 0 = the page is not ready. */
static int read_ns(int realtime, uint64_t *out) {
    const volatile struct vdso_data *d = &vvar_page;
    for (;;) {
        uint32_t s = __atomic_load_n(&d->seq, __ATOMIC_ACQUIRE);
        if (!(s & 1)) {
            if (!d->ready) return 0;
            uint64_t tsc0 = d->tsc0, mult = d->ns_mult, base = d->realtime_base_ns;
            uint64_t now = rdtsc();
            __atomic_thread_fence(__ATOMIC_ACQUIRE);
            if (__atomic_load_n(&d->seq, __ATOMIC_RELAXED) == s) {
                uint64_t mono = (uint64_t)(((unsigned __int128)(now - tsc0) * mult) >> 32);
                *out = realtime ? base + mono : mono;
                return 1;
            }
        }
        __asm__ volatile("pause");
    }
}

int __vdso_clock_gettime(long clk, struct timespec *ts) {
    int realtime;
    switch (clk) {
    case CLOCK_REALTIME: case CLOCK_REALTIME_COARSE: realtime = 1; break;
    case CLOCK_MONOTONIC: case CLOCK_MONOTONIC_RAW: case CLOCK_MONOTONIC_COARSE: case CLOCK_BOOTTIME:
        realtime = 0; break;
    default: return (int)sys2(SYS_clock_gettime, clk, (long)ts);
    }
    uint64_t ns;
    if (!read_ns(realtime, &ns)) return (int)sys2(SYS_clock_gettime, clk, (long)ts);
    ts->tv_sec = (long)(ns / NS_PER_S);
    ts->tv_nsec = (long)(ns % NS_PER_S);
    return 0;
}

int __vdso_gettimeofday(struct timeval *tv, struct timezone *tz) {
    uint64_t ns;
    if (!read_ns(1, &ns)) return (int)sys2(SYS_gettimeofday, (long)tv, (long)tz);
    if (tv) {
        tv->tv_sec = (long)(ns / NS_PER_S);
        tv->tv_usec = (long)(ns % NS_PER_S / 1000);
    }
    if (tz) tz->tz_minuteswest = tz->tz_dsttime = 0;   /* UTC, as the system call says */
    return 0;
}

long __vdso_time(long *t) {
    uint64_t ns;
    if (!read_ns(1, &ns)) return sys2(SYS_time, (long)t, 0);
    long s = (long)(ns / NS_PER_S);
    if (t) *t = s;
    return s;
}

/* The CPU index the nucleus put in IA32_TSC_AUX; one NUMA node. */
int __vdso_getcpu(unsigned *cpu, unsigned *node, void *unused) {
    (void)unused;
    uint32_t lo, hi, aux;
    __asm__ volatile("rdtscp" : "=a"(lo), "=d"(hi), "=c"(aux));
    if (cpu) *cpu = aux;
    if (node) *node = 0;
    return 0;
}

int clock_gettime(long, struct timespec *) __attribute__((weak, alias("__vdso_clock_gettime")));
int gettimeofday(struct timeval *, struct timezone *) __attribute__((weak, alias("__vdso_gettimeofday")));
long time(long *) __attribute__((weak, alias("__vdso_time")));
int getcpu(unsigned *, unsigned *, void *) __attribute__((weak, alias("__vdso_getcpu")));
