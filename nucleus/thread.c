/* thread.c - creating, joining and finding threads, and each CPU's first
 * thread (sched.h). Slots change state only under g_sched_lock. A slot's
 * kernel stack is unmapped and freed at join only when the joining CPU is
 * the only one that ever touched it (its invlpg is then enough); otherwise
 * it stays mapped for the slot's next thread (W20-S06), since another CPU
 * may hold a stale TLB entry until shootdown exists (S07). Never switches
 * threads itself (sched.c does). ADR-0011. */
#include "sched_priv.h"
#include "trap.h"
#include "fpu.h"
#include "percpu.h"
#include "gdt.h"
#include "time.h"
#include "vmm.h"
#include "frames.h"
#include "cpu.h"
#include "kprintf.h"
#include "panic.h"

#define MSR_FS_BASE 0xC0000100u

extern void thread_start(void), thread_start_frame(void);          /* switch.asm */
extern char boot_stack[], boot_stack_top[];                         /* entry.asm */

void thread_bootstrap(void (*fn)(void *), void *arg);   /* from switch.asm */
void thread_bootstrap(void (*fn)(void *), void *arg) {
    sched_start_tail();                 /* first run: from schedule_locked, lock held, IF=0 */
    cpu_sti();
    fn(arg);
    thread_exit();
}

static void idle_main(void *arg) { (void)arg; sched_idle(); }

/* Lock held. */
static struct thread *alloc_slot(void) {
    for (uint32_t i = 0; i < THREADS_MAX; i++)
        if (g_threads[i].state == T_FREE) return &g_threads[i];
    return 0;
}

/* Maps the slot's zeroed kernel stack once; later threads in the slot reuse it. 0 ok. */
static int map_kstack(struct thread *t) {
    uint64_t base = KSTACK_REGION + (uint64_t)(t - g_threads) * KSTACK_SLOT + PAGE_4K;   /* guard below */
    if (t->kstack_bottom == base) return 0;
    for (uint64_t off = 0; off < KSTACK_BYTES; off += PAGE_4K) {
        uint64_t pa = frame_alloc_zeroed();
        if (!pa || vm_map(vmm_kernel(), base + off, pa, PAGE_4K, PTE_W | PTE_NX) != PT_OK) {
            if (pa) frame_free(pa);
            if (off) vm_unmap(vmm_kernel(), base, off, 1);   /* never ran: no CPU caches it */
            return -1;
        }
    }
    t->kstack_bottom = base;
    t->kstack_top = base + KSTACK_BYTES;
    t->stack_cpus = 0;                  /* fresh: no CPU has cached it */
    return 0;
}

static void reset_fields(struct thread *t, const char *name, int pin) {
    t->fs_base = 0;
    t->cr3 = vmm_kernel()->pt.root;
    t->uspace = 0;
    t->killed = 0;
    t->req_state = 0;
    t->req_next = 0;
    t->uframe = 0;
    t->interrupt = 0;
    t->timed_out = 0;
    t->wait_next = 0;
    t->name = name;
    t->pin_cpu = pin;
    t->cpu = 0;                         /* stack_cpus stays: a kept stack keeps its set */
    fpu_init_area(t->fx);
}

/* A slot with a kernel stack, claimed but not runnable yet. */
static struct thread *thread_alloc(const char *name, int pin) {
    uint64_t f = spin_lock_irqsave(&g_sched_lock);
    struct thread *t = alloc_slot();
    if (t) {                            /* claim the slot; not runnable yet, and   */
        t->wake_ns = 0;                 /* no stale timeout may let a tick wake it */
        t->state = T_BLOCKED;
        t->is_idle = 0;
    }
    spin_unlock_irqrestore(&g_sched_lock, f);
    if (!t) return 0;
    if (map_kstack(t) != 0) {
        f = spin_lock_irqsave(&g_sched_lock);
        t->state = T_FREE;
        spin_unlock_irqrestore(&g_sched_lock, f);
        return 0;
    }
    reset_fields(t, name, pin);
    return t;
}

/* sp: the stack switch_to will pop from (6 registers, then a return address). */
static struct thread *thread_publish(struct thread *t, uint64_t *sp) {
    t->rsp = (uint64_t)(uintptr_t)sp;
    uint64_t f = spin_lock_irqsave(&g_sched_lock);
    t->id = g_next_id++;
    t->state = T_RUNNABLE;
    sched_kick(t);
    spin_unlock_irqrestore(&g_sched_lock, f);
    return t;
}

struct thread *thread_create(const char *name, void (*fn)(void *), void *arg) {
    struct thread *t = thread_alloc(name, PIN_ANY);
    if (!t) return 0;
    uint64_t f = irq_save();            /* one CPU writes the stack: the one recorded */
    t->stack_cpus |= 1ull << this_cpu()->index;
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
    irq_restore(f);
    return thread_publish(t, sp);
}

struct thread *thread_create_from_frame(const char *name, const struct trap_frame *frame,
                                        uint64_t fs_base) {
    return thread_create_user(name, frame, fs_base, vmm_kernel()->pt.root, 0, 0);
}

/* Go and user threads stay on CPU 0 until TLB shootdown (W20-S07). */
struct thread *thread_create_user(const char *name, const struct trap_frame *frame,
                                  uint64_t fs_base, uint64_t cr3, uint64_t uspace,
                                  const uint8_t *fx) {
    struct thread *t = thread_alloc(name, 0);
    if (!t) return 0;
    if (fx) fpu_set_legacy(t->fx, fx);   /* a fork child keeps its parent's x87/SSE state */
    t->cr3 = cr3;                       /* before thread_publish: it may run at once */
    t->uspace = uspace;
    /* [top - frame] the trap frame  [below] thread_start_frame  then 6 zero
     * registers: switch_to pops, "returns" into thread_start_frame, which
     * drops the scheduler's lock and goes to trap_resume with RSP at the frame. */
    uint64_t f = irq_save();            /* one CPU writes the stack: the one recorded */
    t->stack_cpus |= 1ull << this_cpu()->index;
    struct trap_frame *tf = (struct trap_frame *)(uintptr_t)(t->kstack_top - sizeof *tf);
    *tf = *frame;
    uint64_t *sp = (uint64_t *)tf;
    *--sp = (uint64_t)(uintptr_t)thread_start_frame;
    for (int i = 0; i < 6; i++) *--sp = 0;
    irq_restore(f);
    t->fs_base = fs_base;
    return thread_publish(t, sp);
}

/* Lock held, IF=0: the code running on this CPU becomes thread t. */
static void adopt(struct thread *t, const char *name, uint64_t bottom, uint64_t top) {
    struct percpu *c = this_cpu();
    reset_fields(t, name, (int)c->index);
    t->state = T_RUNNING;
    t->id = g_next_id++;
    t->cpu = c->index;
    t->kstack_bottom = bottom;
    t->kstack_top = top;
    c->current = t;
    c->kstack_top = top;
    c->kstack_bottom = bottom;
    tss_set_rsp0(top);
}

void sched_init(void) {
    spin_lock(&g_sched_lock);           /* IF is still off at boot */
    adopt(&g_threads[0], "main", (uint64_t)(uintptr_t)boot_stack, (uint64_t)(uintptr_t)boot_stack_top);
    spin_unlock(&g_sched_lock);         /* main stays on CPU 0: the boot flow, ktest_run */
    struct thread *idle = thread_create("idle", idle_main, 0);
    if (!idle) panic("cannot create the idle thread");
    idle->pin_cpu = 0;
    idle->is_idle = 1;                  /* only ever chosen by pick_next's fallback */
    this_cpu()->idle = idle;
    time_set_tick_hook(sched_on_tick);
    cpu_sti();
    kprintf("[nucleus] sched ok (threads: main, idle; %u ms slice; irqs on)\n", TIMESLICE_TICKS);
}

void sched_init_cpu(uint64_t kstack_bottom, uint64_t kstack_top) {
    spin_lock(&g_sched_lock);           /* an AP, IF=0 */
    struct thread *t = alloc_slot();
    if (!t) panic("no thread slot for cpu %u's idle thread", this_cpu()->index);
    adopt(t, "idle", kstack_bottom, kstack_top);
    t->is_idle = 1;
    this_cpu()->idle = t;
    spin_unlock(&g_sched_lock);
}

/* IF=0, so this CPU cannot change under it: frees the stack if only this
 * CPU ever touched it (the unmap's invlpg covers every cached entry). */
static void release_stack(struct thread *t) {
    if (t->stack_cpus & ~(1ull << this_cpu()->index)) return;   /* kept for the slot's next thread */
    vm_unmap(vmm_kernel(), t->kstack_bottom, KSTACK_BYTES, 1);
    t->kstack_bottom = t->kstack_top = 0;
}

void thread_join(struct thread *t) {
    for (;;) {
        uint64_t f = irq_save();
        spin_lock(&g_sched_lock);
        int dead = t->state == T_DEAD;  /* under the lock: its CPU has left its stack */
        spin_unlock(&g_sched_lock);
        if (dead) {
            release_stack(t);
            spin_lock(&g_sched_lock);
            t->state = T_FREE;
            spin_unlock(&g_sched_lock);
        }
        irq_restore(f);
        if (dead) return;
        thread_yield();
    }
}

struct thread *thread_find(uint32_t id) {
    struct thread *found = 0;
    uint64_t f = spin_lock_irqsave(&g_sched_lock);
    for (uint32_t i = 0; i < THREADS_MAX && !found; i++)
        if (g_threads[i].state != T_FREE && g_threads[i].id == id) found = &g_threads[i];
    spin_unlock_irqrestore(&g_sched_lock, f);
    return found;
}

void thread_set_fs_base(struct thread *t, uint64_t fs) {
    uint64_t f = irq_save();
    t->fs_base = fs;
    if (t == this_cpu()->current) wrmsr(MSR_FS_BASE, fs);
    irq_restore(f);
}
