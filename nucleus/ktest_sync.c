/* ktest_sync.c - spinlocks and futexes under real preemption. */
#include "ktest.h"
#include "futex.h"
#include "spinlock.h"
#include "sched.h"
#include "time.h"
#include "errno.h"
#include "kprintf.h"

#define ROUNDS 10000u

static struct spinlock g_lock = SPINLOCK_INIT("ktest-counter");
static volatile uint64_t g_counter;

static void bump(void *arg) {
    (void)arg;
    for (int i = 0; i < 200000; i++) {
        uint64_t f = spin_lock_irqsave(&g_lock);
        uint64_t v = g_counter;               /* read-modify-write that preemption could split */
        g_counter = v + 1;
        spin_unlock_irqrestore(&g_lock, f);
    }
}

int kt_spinlock_counter(void) {
    g_counter = 0;
    struct thread *a = thread_create("bump-a", bump, 0), *b = thread_create("bump-b", bump, 0);
    KT_CHECK(a && b);
    thread_join(a);
    thread_join(b);
    KT_CHECK(g_counter == 400000);
    return 0;
}

static uint32_t g_word;                       /* 0: A's turn, 1: B's turn */
static volatile uint32_t g_trips;

static void player(void *arg) {
    uint32_t mine = (uint32_t)(uintptr_t)arg;
    for (uint32_t i = 0; i < ROUNDS; i++) {
        while (__atomic_load_n(&g_word, __ATOMIC_ACQUIRE) != mine)
            futex_wait(&g_word, mine ^ 1, 0);   /* sleeps while it is the other's turn */
        if (mine == 1) g_trips++;
        __atomic_store_n(&g_word, mine ^ 1, __ATOMIC_RELEASE);
        futex_wake(&g_word, 1);
    }
}

/* 10,000 round trips, each a wait + wake in both directions (PIT-23: a lost
 * wake-up would hang this test, which the smoke timeout then reports). */
int kt_futex_ping_pong(void) {
    g_word = 0;
    g_trips = 0;
    uint64_t t0 = time_monotonic_ns(), s0 = sched_switch_count();
    struct thread *a = thread_create("futex-a", player, (void *)0);
    struct thread *b = thread_create("futex-b", player, (void *)1);
    KT_CHECK(a && b);
    thread_join(a);
    thread_join(b);
    KT_CHECK(g_trips == ROUNDS);
    kprintf("[ktest] futex: %u round trips in %llu ms, %llu switches\n", ROUNDS,
            (unsigned long long)((time_monotonic_ns() - t0) / 1000000),
            (unsigned long long)(sched_switch_count() - s0));
    return 0;
}

int kt_futex_timeout_and_eagain(void) {
    static uint32_t word = 7;
    KT_CHECK(futex_wait(&word, 8, 0) == -EAGAIN);                /* value already changed */
    uint64_t t0 = time_monotonic_ns();
    KT_CHECK(futex_wait(&word, 7, 5000000) == -ETIMEDOUT);       /* nobody wakes: 5 ms */
    uint64_t waited = time_monotonic_ns() - t0;
    KT_CHECK(waited >= 5000000 && waited < 100000000);
    KT_CHECK(futex_wake(&word, 10) == 0);                        /* queue is empty again */
    return 0;
}
