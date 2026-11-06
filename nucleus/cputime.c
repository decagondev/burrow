/* cputime.c - CPU time (W23-S02; cputime.h). A thread's time since its
 * mark goes to one bucket: idle for a CPU's idle thread, user while a user
 * thread is in ring 3, kernel otherwise (the nucleus and the Go kernel);
 * the CPU's own counters get the same. Counters only grow, each written by
 * the one CPU running the thread (IF=0) and read whole with 64-bit loads; a
 * reader sees time up to the last tick, never part of a running stint, so
 * every value it reads is at least the last one. Never takes a lock. */
#include "cputime.h"
#include "sched_priv.h"
#include "percpu.h"
#include "smp.h"
#include "time.h"
#include "cpu.h"

static struct cputime_cpu g_cpu_times[SMP_MAX_CPUS];

static void add(uint64_t *counter, uint64_t ns) {
    __atomic_store_n(counter, __atomic_load_n(counter, __ATOMIC_RELAXED) + ns, __ATOMIC_RELAXED);
}

/* IF=0, on the CPU running t: t's time since its mark goes to its bucket. */
static void charge(struct thread *t, uint64_t now) {
    uint64_t mark = t->acct_mark, ns = now > mark ? now - mark : 0;
    struct cputime_cpu *c = &g_cpu_times[this_cpu()->index];
    t->acct_mark = now;
    if (!mark) return;                  /* its first stint starts now */
    if (t->is_idle) {
        add(&t->sys_ns, ns);
        add(&c->idle_ns, ns);
    } else if (t->in_user) {
        add(&t->user_ns, ns);
        add(&c->user_ns, ns);
    } else {
        add(&t->sys_ns, ns);
        add(&c->sys_ns, ns);
    }
}

void cputime_reset(struct thread *t, int in_user) {
    __atomic_store_n(&t->user_ns, 0, __ATOMIC_RELAXED);
    __atomic_store_n(&t->sys_ns, 0, __ATOMIC_RELAXED);
    t->acct_mark = 0;
    t->in_user = in_user;
}

void cputime_switch(struct thread *prev, struct thread *next, uint64_t now_ns) {
    charge(prev, now_ns);
    next->acct_mark = now_ns;
    add(&g_cpu_times[this_cpu()->index].switches, 1);
}

void cputime_tick(struct thread *cur, uint64_t now_ns) { charge(cur, now_ns); }

void cputime_enter_kernel(struct thread *t) {
    uint64_t f = irq_save();
    charge(t, time_monotonic_ns());
    t->in_user = 0;
    irq_restore(f);
}

void cputime_enter_user(struct thread *t) {
    uint64_t f = irq_save();
    charge(t, time_monotonic_ns());
    t->in_user = 1;
    irq_restore(f);
}

void cputime_thread(struct thread *t, uint64_t *user_ns, uint64_t *sys_ns) {
    *user_ns = __atomic_load_n(&t->user_ns, __ATOMIC_RELAXED);
    *sys_ns = __atomic_load_n(&t->sys_ns, __ATOMIC_RELAXED);
}

uint32_t cputime_cpus(struct cputime_cpu *out, uint32_t max) {
    uint32_t n = smp_cpu_count();
    if (n > max) n = max;
    for (uint32_t i = 0; i < n; i++) {
        out[i].user_ns = __atomic_load_n(&g_cpu_times[i].user_ns, __ATOMIC_RELAXED);
        out[i].sys_ns = __atomic_load_n(&g_cpu_times[i].sys_ns, __ATOMIC_RELAXED);
        out[i].idle_ns = __atomic_load_n(&g_cpu_times[i].idle_ns, __ATOMIC_RELAXED);
        out[i].switches = __atomic_load_n(&g_cpu_times[i].switches, __ATOMIC_RELAXED);
    }
    return n;
}
