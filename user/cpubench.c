/* cpubench.c - CPU-bound work in four threads (W20-S08): each runs the same
 * fixed loop on its own data, with no shared writes and no system calls
 * inside it, and the program prints the wall time and the CPUs online
 * (sysconf, which musl answers from sched_getaffinity). make smp-bench runs
 * it on one CPU and on four; tests/smoke/smp-budgets.py wants four CPUs at
 * least 2.5 times faster. Never sleeps or yields in the loop.
 * Docs: docs/04-plan/waves/WAVE-20-smp.md. */
#define _GNU_SOURCE
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <time.h>
#include <unistd.h>

#define THREADS 4
#define ROUNDS  120000000ul

static uint64_t results[THREADS * 8];   /* a cache line apart: no false sharing */

static void *work(void *arg) {
    uintptr_t k = (uintptr_t)arg;
    uint64_t x = 0x9E3779B97F4A7C15ull ^ k;
    for (unsigned long i = 0; i < ROUNDS; i++) {   /* xorshift64 */
        x ^= x << 13;
        x ^= x >> 7;
        x ^= x << 17;
    }
    results[k * 8] = x;
    return 0;
}

static long now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
}

int main(void) {
    pthread_t t[THREADS];
    long t0 = now_ms();
    for (uintptr_t k = 0; k < THREADS; k++)
        if (pthread_create(&t[k], 0, work, (void *)k) != 0) { puts("cpubench: FAIL pthread_create"); return 1; }
    for (int k = 0; k < THREADS; k++) pthread_join(t[k], 0);
    long ms = now_ms() - t0;
    for (int k = 1; k < THREADS; k++)
        if (results[k * 8] == results[0]) { puts("cpubench: FAIL threads computed the same"); return 1; }
    printf("cpubench: %d threads x %lu rounds in %ld ms on %ld CPUs\n", THREADS, ROUNDS, ms,
           sysconf(_SC_NPROCESSORS_ONLN));
    return 0;
}
