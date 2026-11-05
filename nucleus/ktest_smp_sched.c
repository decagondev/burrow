/* ktest_smp_sched.c - threads on every CPU (W20-S06, ADR-0011), suite "smp"
 * on -smp 4: every CPU runs threads from the one run queue; futex
 * ping-pong across CPUs loses no wake-up; a thread that moves between CPUs
 * keeps its FS base and its SSE registers (PIT-22's list, per CPU). */
#include "ktest.h"
#include "sched.h"
#include "smp.h"
#include "percpu.h"
#include "futex.h"
#include "time.h"
#include "cpu.h"
#include "kprintf.h"

#define MSR_FS_BASE      0xC0000100u
#define EVERYWHERE_NS    2000000000ull
#define PAIRS            4u
#define PAIR_ROUNDS      20000u
#define MOVERS           4u
#define MOVER_STEPS      200u

static uint32_t popcount64(uint64_t v) { uint32_t n = 0; for (; v; v &= v - 1) n++; return n; }
static uint64_t all_cpus(void) { return (1ull << smp_cpu_count()) - 1; }

static uint64_t g_ran_on;               /* bit per CPU index, set atomically */

static void everywhere(void *arg) {
    (void)arg;
    uint64_t until = time_monotonic_ns() + EVERYWHERE_NS;
    while ((__atomic_load_n(&g_ran_on, __ATOMIC_RELAXED) & all_cpus()) != all_cpus()
           && time_monotonic_ns() < until)
        __atomic_fetch_or(&g_ran_on, 1ull << this_cpu()->index, __ATOMIC_RELAXED);
}

/* As many busy threads as CPUs: each CPU runs one of them. */
int kt_smp_threads_everywhere(void) {
    struct thread *t[SMP_MAX_CPUS];
    uint32_t n = smp_cpu_count();
    g_ran_on = 0;
    for (uint32_t i = 0; i < n; i++) KT_CHECK((t[i] = thread_create("everywhere", everywhere, 0)) != 0);
    for (uint32_t i = 0; i < n; i++) thread_join(t[i]);
    KT_CHECK(n > 1 && (g_ran_on & all_cpus()) == all_cpus());
    return 0;
}

struct pair { uint32_t word; volatile uint32_t trips; };
static struct pair g_pairs[PAIRS];

static void pair_player(void *arg) {
    uint32_t v = (uint32_t)(uintptr_t)arg, mine = v & 1;
    struct pair *p = &g_pairs[v >> 1];
    for (uint32_t i = 0; i < PAIR_ROUNDS; i++) {
        while (__atomic_load_n(&p->word, __ATOMIC_ACQUIRE) != mine)
            futex_wait(&p->word, mine ^ 1, 0);
        if (mine) p->trips++;
        __atomic_fetch_or(&g_ran_on, 1ull << this_cpu()->index, __ATOMIC_RELAXED);
        __atomic_store_n(&p->word, mine ^ 1, __ATOMIC_RELEASE);
        futex_wake(&p->word, 1);
    }
}

/* Four pairs, each 20,000 round trips through one futex word: a waker and a
 * sleeper on different CPUs must never lose a wake-up (the test would hang). */
int kt_smp_futex_ping_pong(void) {
    struct thread *t[2 * PAIRS];
    uint64_t t0 = time_monotonic_ns();
    g_ran_on = 0;
    for (uint32_t i = 0; i < PAIRS; i++) g_pairs[i].word = g_pairs[i].trips = 0;
    for (uint32_t i = 0; i < 2 * PAIRS; i++)
        KT_CHECK((t[i] = thread_create("pair", pair_player, (void *)(uintptr_t)i)) != 0);
    for (uint32_t i = 0; i < 2 * PAIRS; i++) thread_join(t[i]);
    for (uint32_t i = 0; i < PAIRS; i++) KT_CHECK(g_pairs[i].trips == PAIR_ROUNDS);
    KT_CHECK(popcount64(g_ran_on) > 1);
    kprintf("[ktest] smp futex: %u pairs x %u round trips in %llu ms on %u CPUs\n", PAIRS, PAIR_ROUNDS,
            (unsigned long long)((time_monotonic_ns() - t0) / 1000000), popcount64(g_ran_on));
    return 0;
}

extern void ktest_set_xmm0(uint64_t v);
extern uint64_t ktest_get_xmm0(void);
static uint64_t g_mover_cpus[MOVERS];
static volatile int g_mover_ok[MOVERS];

static void mover(void *arg) {
    uint32_t k = (uint32_t)(uintptr_t)arg;
    uint64_t fs = 0x10000ull * (k + 1), xmm = 0x5151000000000000ull | k;
    thread_set_fs_base(thread_current(), fs);
    ktest_set_xmm0(xmm);
    for (uint32_t i = 0; i < MOVER_STEPS; i++) {
        if (i % 4 == 0) thread_sleep_ns(1000000);   /* woken by CPU 0's tick, run anywhere */
        else thread_yield();
        g_mover_cpus[k] |= 1ull << this_cpu()->index;
        if (rdmsr(MSR_FS_BASE) != fs || ktest_get_xmm0() != xmm) return;
    }
    g_mover_ok[k] = 1;
}

/* Threads that sleep and yield move between CPUs, and each keeps its own
 * FS base and xmm0 wherever it lands. */
int kt_smp_migration(void) {
    struct thread *t[MOVERS];
    for (uint32_t k = 0; k < MOVERS; k++) g_mover_cpus[k] = 0, g_mover_ok[k] = 0;
    for (uint32_t k = 0; k < MOVERS; k++)
        KT_CHECK((t[k] = thread_create("mover", mover, (void *)(uintptr_t)k)) != 0);
    for (uint32_t k = 0; k < MOVERS; k++) thread_join(t[k]);
    uint32_t moved = 0;
    for (uint32_t k = 0; k < MOVERS; k++) {
        KT_CHECK(g_mover_ok[k]);
        if (popcount64(g_mover_cpus[k]) > 1) moved++;
    }
    KT_CHECK(moved > 0);
    KT_CHECK(rdmsr(MSR_FS_BASE) == 0);      /* main's own FS base is back */
    return 0;
}
