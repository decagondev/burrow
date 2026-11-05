/* sched.c - switching, blocking, waking and the tick, on every CPU from one
 * run queue (W20-S06, ADR-0011). g_sched_lock is taken with IF=0 and held
 * across switch_to: the thread switched to releases it (its own caller of
 * schedule_locked, or thread_bootstrap / sched_start_tail when it is new),
 * so no CPU can pick a thread whose registers another CPU is still saving.
 * Lock order: an object's lock (futex bucket, irq, uthread FIFO) first,
 * then g_sched_lock; never another lock while holding it. Thread creation
 * and lookup live in thread.c. */
#include "sched_priv.h"
#include "fpu.h"
#include "percpu.h"
#include "gdt.h"
#include "time.h"
#include "apic.h"
#include "smp.h"
#include "cpu.h"
#include "panic.h"
#include "syscall.h"

#define MSR_FS_BASE 0xC0000100u

extern void switch_to(struct thread *prev, struct thread *next);   /* switch.asm */

struct thread g_threads[THREADS_MAX];
struct spinlock g_sched_lock = SPINLOCK_INIT("sched");
uint32_t g_next_id;
static uint64_t g_switches;
static uint32_t g_cursor;           /* where a search from an idle CPU starts */

struct thread *thread_current(void) { return (struct thread *)(uintptr_t)read_gs_qword(16); }   /* one load: percpu.current */
uint32_t thread_id(const struct thread *t) { return t->id; }
uint64_t sched_switch_count(void) { return __atomic_load_n(&g_switches, __ATOMIC_RELAXED); }

static int can_run_on(const struct thread *t, uint32_t cpu) {
    return !t->is_idle && (t->pin_cpu == PIN_ANY || (uint32_t)t->pin_cpu == cpu);
}

/* Next runnable thread for CPU c after `from` in slot order; from again if it
 * may go on, else c's idle thread. */
static struct thread *pick_next(struct percpu *c, struct thread *from) {
    uint32_t start = from->is_idle ? g_cursor : (uint32_t)(from - g_threads);
    for (uint32_t k = 1; k <= THREADS_MAX; k++) {
        uint32_t i = (start + k) % THREADS_MAX;
        struct thread *t = &g_threads[i];
        if (t->state == T_RUNNABLE && can_run_on(t, c->index)) {
            g_cursor = i;
            return t;
        }
    }
    return from->state == T_RUNNING ? from : c->idle;
}

/* IF=0, g_sched_lock held; returns (in prev, maybe on another CPU) with it
 * held again. PIT-22's list, all of it per CPU. */
static void schedule_locked(void) {
    struct percpu *c = this_cpu();
    struct thread *prev = c->current, *next = pick_next(c, prev);
    c->slice = 0;
    if (next == prev) {                 /* also: woken again before it could switch out */
        prev->state = T_RUNNING;
        return;
    }
    if (prev->state == T_RUNNING) prev->state = T_RUNNABLE;
    next->state = T_RUNNING;
    next->cpu = c->index;
    next->stack_cpus |= 1ull << c->index;
    c->current = next;
    c->kstack_top = next->kstack_top;
    c->kstack_bottom = next->kstack_bottom;
    tss_set_rsp0(next->kstack_top);
    if (next->fs_base != prev->fs_base) wrmsr(MSR_FS_BASE, next->fs_base);
    if (next->cr3 != prev->cr3) write_cr3(next->cr3);   /* flushes the TLB's user half */
    if ((next->uspace != 0) != c->lstar_user) syscall_set_entry(next->uspace != 0);   /* ADR-0011 */
    fpu_save(prev->fx);
    fpu_restore(next->fx);
    g_switches++;
    switch_to(prev, next);              /* returns when prev is picked again, on any CPU */
}

/* A new thread's first instructions run here with the lock still held. */
void sched_start_tail(void) { spin_unlock(&g_sched_lock); }

/* Lock held: an IPI to one idle CPU that may run t (not this one: its own
 * idle loop or tick finds t). */
void sched_kick(const struct thread *t) {
    uint32_t self = this_cpu()->index, n = smp_cpu_count();
    for (uint32_t i = 0; i < n; i++) {
        struct percpu *c = &g_cpus[i];
        if (i == self || !c->idle || c->current != c->idle || !can_run_on(t, i)) continue;
        lapic_send_ipi((uint8_t)c->apic_id, VEC_WAKE);
        return;
    }
}

static void wake_sleepers(void) {
    uint64_t now = time_monotonic_ns();
    for (uint32_t i = 0; i < THREADS_MAX; i++) {
        struct thread *t = &g_threads[i];
        if ((t->state == T_SLEEPING || t->state == T_BLOCKED) && t->wake_ns && now >= t->wake_ns) {
            if (t->state == T_BLOCKED) t->timed_out = 1;
            t->wake_ns = 0;
            t->state = T_RUNNABLE;
            sched_kick(t);
        }
    }
}

/* Every CPU's timer tick (IRQ context, IF=0). CPU 0 wakes the sleepers. */
void sched_on_tick(void) {
    struct percpu *c = this_cpu();
    if (!c->idle) return;               /* this CPU does not schedule yet */
    spin_lock(&g_sched_lock);
    if (c->index == 0) wake_sleepers();
    if (++c->slice >= TIMESLICE_TICKS || c->current == c->idle) schedule_locked();
    spin_unlock(&g_sched_lock);
}

/* Every CPU's idle thread: run the mailbox (smp.c), any runnable thread,
 * else hlt. The check and sti;hlt are one step: a kick that lands after
 * the check is taken right after sti and ends the hlt. */
void sched_idle(void) {
    for (;;) {
        cpu_cli();
        smp_mailbox_run();
        spin_lock(&g_sched_lock);
        uint64_t before = g_switches;
        schedule_locked();
        int ran = g_switches != before;
        spin_unlock(&g_sched_lock);
        if (ran) cpu_sti();             /* there may be more: look again */
        else cpu_sti_hlt();
    }
}

void thread_yield(void) {
    uint64_t f = irq_save();
    spin_lock(&g_sched_lock);
    schedule_locked();
    spin_unlock(&g_sched_lock);
    irq_restore(f);
}

static void mark_blocked(uint64_t timeout_ns) {
    struct thread *t = this_cpu()->current;
    t->timed_out = 0;
    t->wake_ns = timeout_ns ? time_monotonic_ns() + timeout_ns : 0;
    t->state = T_BLOCKED;
}

void thread_block(uint64_t timeout_ns) {
    spin_lock(&g_sched_lock);
    mark_blocked(timeout_ns);
    schedule_locked();
    spin_unlock(&g_sched_lock);
}

/* Blocked under g_sched_lock before `lock` drops: a waker on any CPU takes
 * `lock`, then g_sched_lock, and finds us blocked (PIT-23). */
void thread_block_unlock(uint64_t timeout_ns, struct spinlock *lock) {
    spin_lock(&g_sched_lock);
    mark_blocked(timeout_ns);
    spin_unlock(lock);
    schedule_locked();
    spin_unlock(&g_sched_lock);
}

void thread_wake(struct thread *t) {
    uint64_t f = spin_lock_irqsave(&g_sched_lock);
    if (t->state == T_BLOCKED) {
        t->wake_ns = 0;
        t->state = T_RUNNABLE;
        sched_kick(t);
    }
    spin_unlock_irqrestore(&g_sched_lock, f);
}

void thread_sleep_ns(uint64_t ns) {
    uint64_t f = irq_save();
    spin_lock(&g_sched_lock);
    struct thread *t = this_cpu()->current;
    t->wake_ns = time_monotonic_ns() + (ns ? ns : 1);
    t->state = T_SLEEPING;
    schedule_locked();
    spin_unlock(&g_sched_lock);
    irq_restore(f);
}

void thread_exit(void) {
    cpu_cli();
    spin_lock(&g_sched_lock);
    this_cpu()->current->state = T_DEAD;   /* thread_join frees the slot under the lock */
    schedule_locked();
    panic("dead thread was scheduled");
}
