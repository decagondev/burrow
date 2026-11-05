/* ktest_sched.c - threads: cooperative alternation, preemption, sleeping,
 * per-thread FS_BASE, guard pages. */
#include "fpu.h"
#include "ktest.h"
#include "sched.h"
#include "time.h"
#include "trap.h"
#include "cpu.h"
#include "kprintf.h"

#define MSR_FS_BASE 0xC0000100u

static volatile char g_log[32];
static volatile unsigned g_len;
static volatile char g_turn;

/* Waits (yielding) for its turn, so preemption cannot reorder the steps. */
static void pinger(void *arg) {
    char c = (char)(uintptr_t)arg;
    for (int i = 0; i < 5; i++) {
        while (g_turn != c) thread_yield();
        kprintf("[ktest] %s %d\n", c == 'A' ? "ping" : "pong", i);
        g_log[g_len++] = c;
        g_turn = c == 'A' ? 'B' : 'A';
        thread_yield();
    }
}

/* Two threads hand the CPU back and forth with thread_yield 5 times each. */
int kt_sched_ping_pong(void) {
    g_len = 0;
    g_turn = 'A';
    struct thread *a = thread_create("ping", pinger, (void *)'A');
    struct thread *b = thread_create("pong", pinger, (void *)'B');
    KT_CHECK(a && b);
    thread_join(a);
    thread_join(b);
    KT_CHECK(g_len == 10);
    for (unsigned i = 0; i < 10; i++) KT_CHECK(g_log[i] == (i % 2 ? 'B' : 'A'));
    return 0;
}

static volatile int g_flag;

static void spinner(void *arg) {           /* never yields: only preemption lets others run */
    (void)arg;
    while (!g_flag) { }
}
static void setter(void *arg) {
    (void)arg;
    g_flag = 1;
}

int kt_sched_preempts(void) {
    g_flag = 0;
    uint64_t switches = sched_switch_count();
    struct thread *s = thread_create("spinner", spinner, 0);
    struct thread *t = thread_create("setter", setter, 0);
    KT_CHECK(s && t);
    thread_join(s);                         /* finishes only if setter got CPU time */
    thread_join(t);
    KT_CHECK(g_flag == 1);
    KT_CHECK(sched_switch_count() > switches);
    return 0;
}

static volatile uint64_t g_fs_seen[2];

static void fs_checker(void *arg) {
    uint64_t want = (uint64_t)(uintptr_t)arg;
    thread_set_fs_base(thread_current(), want);
    for (int i = 0; i < 20; i++) {
        if (rdmsr(MSR_FS_BASE) != want) return;  /* lost across a switch: PIT-22 */
        thread_sleep_ns(1000000);           /* 1 ms: forces switches both ways */
    }
    g_fs_seen[want == 0x1000 ? 0 : 1] = want;
}

/* Sleep wakes on time, and FS_BASE follows the thread (PIT-22). */
int kt_sched_sleep_and_fs_base(void) {
    g_fs_seen[0] = g_fs_seen[1] = 0;
    uint64_t t0 = time_monotonic_ns();
    struct thread *a = thread_create("fs-a", fs_checker, (void *)0x1000);
    struct thread *b = thread_create("fs-b", fs_checker, (void *)0x2000);
    KT_CHECK(a && b);
    thread_join(a);
    thread_join(b);
    uint64_t ms = (time_monotonic_ns() - t0) / 1000000;
    KT_CHECK(g_fs_seen[0] == 0x1000 && g_fs_seen[1] == 0x2000);
    KT_CHECK(ms >= 19 && ms < 200);
    KT_CHECK(rdmsr(MSR_FS_BASE) == 0);      /* main's own FS_BASE is back */
    return 0;
}

static volatile int g_guard_hit;

static void guard_prober(void *arg) {
    (void)arg;
    struct thread *me = thread_current();
    uint64_t v;
    g_guard_hit = probe_read64(me->kstack_bottom - 8, &v) == -1 && trap_last.vector == VEC_PF;
}

/* The page under every kernel stack is a guard: touching it faults. */
int kt_sched_stack_guard(void) {
    g_guard_hit = 0;
    struct thread *t = thread_create("guard", guard_prober, 0);
    KT_CHECK(t);
    thread_join(t);
    KT_CHECK(g_guard_hit);
    return 0;
}

extern void ktest_set_xmm0(uint64_t v);
extern uint64_t ktest_get_xmm0(void);
static volatile int g_xmm_ok[2];

static void xmm_holder(void *arg) {
    uint64_t want = (uint64_t)(uintptr_t)arg;
    ktest_set_xmm0(want);
    for (int i = 0; i < 50; i++) {
        thread_yield();                       /* the other thread loads its own xmm0 */
        if (i % 10 == 0) thread_sleep_ns(1000000);
        if (ktest_get_xmm0() != want) return;
    }
    g_xmm_ok[want == 0x1111222233334444ull ? 0 : 1] = 1;
}

extern void ktest_set_ymm0_hi(uint64_t v);
extern uint64_t ktest_get_ymm0_hi(void);
static volatile int g_ymm_ok[2];

static void ymm_holder(void *arg) {
    uint64_t want = (uint64_t)(uintptr_t)arg;
    ktest_set_ymm0_hi(want);
    for (int i = 0; i < 50; i++) {
        thread_yield();                       /* the other thread loads its own ymm0 */
        if (i % 10 == 0) thread_sleep_ns(1000000);
        if (ktest_get_ymm0_hi() != want) return;
    }
    g_ymm_ok[want == 0x1212343456567878ull ? 0 : 1] = 1;
}

/* W20-S03: with XSAVE, two threads keep different upper halves of ymm0
 * (FXSAVE would lose them: they are AVX state). */
int kt_avx_state_per_thread(void) {
    KT_CHECK(g_fpu_xsave && fpu_has_avx());
    g_ymm_ok[0] = g_ymm_ok[1] = 0;
    ktest_set_ymm0_hi(0xbeef);
    struct thread *a = thread_create("ymm-a", ymm_holder, (void *)0x1212343456567878ull);
    struct thread *b = thread_create("ymm-b", ymm_holder, (void *)0x9a9abcbcdedef0f0ull);
    KT_CHECK(a && b);
    thread_join(a);
    thread_join(b);
    KT_CHECK(g_ymm_ok[0] && g_ymm_ok[1]);
    KT_CHECK(ktest_get_ymm0_hi() == 0xbeef);  /* main's value survived too */
    return 0;
}

/* Two threads with different xmm0 values keep them across switches. */
int kt_sse_state_per_thread(void) {
    g_xmm_ok[0] = g_xmm_ok[1] = 0;
    ktest_set_xmm0(0xdead);
    struct thread *a = thread_create("xmm-a", xmm_holder, (void *)0x1111222233334444ull);
    struct thread *b = thread_create("xmm-b", xmm_holder, (void *)0x5555666677778888ull);
    KT_CHECK(a && b);
    thread_join(a);
    thread_join(b);
    KT_CHECK(g_xmm_ok[0] && g_xmm_ok[1]);
    KT_CHECK(ktest_get_xmm0() == 0xdead);     /* main's value survived too */
    return 0;
}
