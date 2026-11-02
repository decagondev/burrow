/* sched.c - see sched.h. All scheduler state is touched with interrupts off. */
#include "sched.h"
#include "percpu.h"
#include "gdt.h"
#include "time.h"
#include "vmm.h"
#include "frames.h"
#include "cpu.h"
#include "kprintf.h"
#include "lib_string.h"
#include "panic.h"

#define MSR_FS_BASE   0xC0000100u
/* Kernel stacks: slot i at KSTACK_REGION + i * KSTACK_SLOT; the first page of
 * each slot stays unmapped (guard). Upper half: shared by every address space. */
#define KSTACK_REGION 0xFFFFFFFFC0000000ull
#define KSTACK_SLOT   (KSTACK_BYTES + PAGE_4K)

extern void switch_to(struct thread *prev, struct thread *next);   /* switch.asm */
extern void thread_start(void);
extern char boot_stack[], boot_stack_top[];                         /* entry.asm */

static struct thread g_threads[THREADS_MAX];
static struct thread *g_idle;
static uint32_t g_next_id;
static uint32_t g_slice;
static uint64_t g_switches;

struct thread *thread_current(void) { return g_percpu.current; }
uint32_t thread_id(const struct thread *t) { return t->id; }
uint64_t sched_switch_count(void) { return g_switches; }

/* Next runnable thread after `from` in slot order; the idle thread only if
 * nothing else can run. */
static struct thread *pick_next(struct thread *from) {
    uint32_t start = (uint32_t)(from - g_threads);
    for (uint32_t k = 1; k <= THREADS_MAX; k++) {
        struct thread *t = &g_threads[(start + k) % THREADS_MAX];
        if (t != g_idle && t->state == T_RUNNABLE) return t;
    }
    return from->state == T_RUNNING ? from : g_idle;
}

/* Interrupts OFF. Picks the next thread and switches to it (PIT-22 order). */
static void schedule(void) {
    struct thread *prev = g_percpu.current, *next = pick_next(prev);
    g_slice = 0;
    if (next == prev) return;
    if (prev->state == T_RUNNING) prev->state = T_RUNNABLE;
    next->state = T_RUNNING;
    g_percpu.current = next;
    g_percpu.kstack_top = next->kstack_top;
    g_percpu.kstack_bottom = next->kstack_bottom;
    tss_set_rsp0(next->kstack_top);
    if (next->fs_base != prev->fs_base) wrmsr(MSR_FS_BASE, next->fs_base);
    g_switches++;
    switch_to(prev, next);              /* returns when prev is picked again */
}

/* Timer tick (IRQ context, interrupts off): wake sleepers, preempt. */
static void on_tick(void) {
    uint64_t now = time_monotonic_ns();
    for (uint32_t i = 0; i < THREADS_MAX; i++) {
        struct thread *t = &g_threads[i];
        if ((t->state == T_SLEEPING || t->state == T_BLOCKED) && t->wake_ns && now >= t->wake_ns) {
            if (t->state == T_BLOCKED) t->timed_out = 1;
            t->wake_ns = 0;
            t->state = T_RUNNABLE;
        }
    }
    if (++g_slice >= TIMESLICE_TICKS || g_percpu.current == g_idle) schedule();
}

void thread_bootstrap(void (*fn)(void *), void *arg);   /* from switch.asm */
void thread_bootstrap(void (*fn)(void *), void *arg) {
    cpu_sti();                          /* first run: we arrived from schedule() with IF=0 */
    fn(arg);
    thread_exit();
}

static void idle_loop(void *arg) {
    (void)arg;
    for (;;) __asm__ volatile("sti; hlt");
}

static struct thread *alloc_slot(void) {
    for (uint32_t i = 0; i < THREADS_MAX; i++)
        if (g_threads[i].state == T_FREE) return &g_threads[i];
    return 0;
}

/* Maps a fresh, zeroed kernel stack into the slot's VA range. 0 ok. */
static int map_kstack(struct thread *t) {
    uint64_t base = KSTACK_REGION + (uint64_t)(t - g_threads) * KSTACK_SLOT + PAGE_4K;   /* guard below */
    for (uint64_t off = 0; off < KSTACK_BYTES; off += PAGE_4K) {
        uint64_t pa = frame_alloc_zeroed();
        if (!pa || vm_map(vmm_kernel(), base + off, pa, PAGE_4K, PTE_W | PTE_NX) != PT_OK) {
            if (pa) frame_free(pa);
            if (off) vm_unmap(vmm_kernel(), base, off, 1);
            return -1;
        }
    }
    t->kstack_bottom = base;
    t->kstack_top = base + KSTACK_BYTES;
    return 0;
}

struct thread *thread_create(const char *name, void (*fn)(void *), void *arg) {
    uint64_t f = irq_save();
    struct thread *t = alloc_slot();
    if (t) {                            /* claim the slot; not runnable yet, and   */
        t->wake_ns = 0;                 /* no stale timeout may let on_tick wake it */
        t->state = T_BLOCKED;
    }
    irq_restore(f);
    if (!t || map_kstack(t) != 0) {
        if (t) t->state = T_FREE;
        return 0;
    }
    /* Initial stack, read by switch_to's pops and ret (see switch.asm):
     * [top-8] 0  [top-16] thread_start  then r15 r14 r13=arg r12=fn rbx rbp */
    uint64_t *sp = (uint64_t *)(uintptr_t)t->kstack_top;
    *--sp = 0;
    *--sp = (uint64_t)(uintptr_t)thread_start;
    *--sp = 0;                          /* rbp */
    *--sp = 0;                          /* rbx */
    *--sp = (uint64_t)(uintptr_t)fn;    /* r12 */
    *--sp = (uint64_t)(uintptr_t)arg;   /* r13 */
    *--sp = 0;                          /* r14 */
    *--sp = 0;                          /* r15 */
    t->rsp = (uint64_t)(uintptr_t)sp;
    t->fs_base = 0;
    t->wake_ns = 0;
    t->timed_out = 0;
    t->wait_next = 0;
    t->name = name;
    f = irq_save();
    t->id = g_next_id++;
    t->state = T_RUNNABLE;
    irq_restore(f);
    return t;
}

void sched_init(void) {
    struct thread *main = &g_threads[0];
    main->state = T_RUNNING;
    main->id = g_next_id++;
    main->name = "main";
    main->kstack_bottom = (uint64_t)(uintptr_t)boot_stack;
    main->kstack_top = (uint64_t)(uintptr_t)boot_stack_top;
    g_percpu.current = main;
    g_percpu.kstack_top = main->kstack_top;
    g_percpu.kstack_bottom = main->kstack_bottom;
    tss_set_rsp0(main->kstack_top);

    g_idle = thread_create("idle", idle_loop, 0);
    if (!g_idle) panic("cannot create the idle thread");
    g_idle->state = T_BLOCKED;          /* only ever chosen by pick_next's fallback */
    time_set_tick_hook(on_tick);
    cpu_sti();
    kprintf("[nucleus] sched ok (threads: main, idle; %u ms slice; irqs on)\n", TIMESLICE_TICKS);
}

void thread_yield(void) {
    uint64_t f = irq_save();
    schedule();
    irq_restore(f);
}

void thread_block(uint64_t timeout_ns) {
    struct thread *t = g_percpu.current;
    t->timed_out = 0;
    t->wake_ns = timeout_ns ? time_monotonic_ns() + timeout_ns : 0;
    t->state = T_BLOCKED;
    schedule();
}

void thread_wake(struct thread *t) {
    if (t->state != T_BLOCKED) return;
    t->wake_ns = 0;
    t->state = T_RUNNABLE;
}

void thread_sleep_ns(uint64_t ns) {
    uint64_t f = irq_save();
    struct thread *t = g_percpu.current;
    t->wake_ns = time_monotonic_ns() + (ns ? ns : 1);
    t->state = T_SLEEPING;
    schedule();
    irq_restore(f);
}

void thread_exit(void) {
    cpu_cli();
    g_percpu.current->state = T_DEAD;
    schedule();
    panic("dead thread was scheduled");
}

void thread_join(struct thread *t) {
    while (t->state != T_DEAD) thread_yield();
    vm_unmap(vmm_kernel(), t->kstack_bottom, KSTACK_BYTES, 1);
    uint64_t f = irq_save();
    t->state = T_FREE;
    irq_restore(f);
}

void thread_set_fs_base(struct thread *t, uint64_t fs) {
    uint64_t f = irq_save();
    t->fs_base = fs;
    if (t == g_percpu.current) wrmsr(MSR_FS_BASE, fs);
    irq_restore(f);
}
