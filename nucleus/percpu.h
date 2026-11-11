/* percpu.h - per-CPU state (W20-S05, ADR-0011): one struct percpu per CPU in
 * g_cpus (entry 0 the BSP), reached through GS_BASE. trap.asm and
 * syscall.asm read the first fields by fixed offset (gs:0, gs:8, gs:24):
 * keep them first and in this order. C reads this CPU's with this_cpu(),
 * never g_cpus[i] for "my" entry. Ring 3 runs with the user's GS base:
 * every entry from ring 3 swaps (swapgs), every exit to it swaps back. */
#ifndef NUCLEUS_PERCPU_H
#define NUCLEUS_PERCPU_H
#include <stdint.h>
#include "cpu.h"

#define SMP_MAX_CPUS     16u
#define PERCPU_SELF_OFF  32u
#define MSR_GS_BASE        0xC0000101u
#define MSR_KERNEL_GS_BASE 0xC0000102u
#define MSR_TSC_AUX        0xC0000103u   /* rdtscp's ECX: the CPU index, for the vDSO's getcpu (W25-S02) */

struct thread;

struct percpu {
    uint64_t kstack_top;      /* +0: current thread's kernel stack, highest address */
    uint64_t kstack_bottom;   /* +8: lowest usable address of that stack            */
    struct thread *current;   /* +16                                                */
    uint64_t syscall_scratch; /* +24: syscall.asm parks one value here (IF=0)       */
    struct percpu *self;      /* +32: this_cpu() reads it with one gs: load         */
    uint32_t index;           /* position in g_cpus                                 */
    uint32_t apic_id;
    int      lstar_user;      /* LSTAR points at syscall_entry_user (sched.c)       */
    int      online;          /* the CPU checked in (smp.c)                         */
    /* AP mailbox (smp.c): set fn, then the wake IPI; the AP runs fn(arg) and sets done */
    void (*volatile fn)(void *);
    void *volatile arg;
    volatile uint64_t done;
    /* scheduler (sched.c, under g_sched_lock) */
    struct thread *idle;      /* this CPU's idle thread; 0 until it schedules     */
    uint32_t slice;           /* ticks the current thread has run                 */
    /* TLB shootdown (tlb.c, W20-S07) */
    uint64_t cr3;             /* the PML4 this CPU runs on (after its mov to CR3) */
    volatile uint32_t shoot_pending;   /* a shootdown for this CPU waits to be done */
    /* idle (sched.c, W20-S08): a kick is a flag; only a halted CPU needs the IPI */
    volatile uint32_t kicked;  /* a thread it may run became runnable            */
    volatile uint32_t halted;  /* in sti;hlt (or about to be): wake it by IPI    */
};
_Static_assert(__builtin_offsetof(struct percpu, kstack_top) == 0, "trap.asm offset");
_Static_assert(__builtin_offsetof(struct percpu, kstack_bottom) == 8, "trap.asm offset");
_Static_assert(__builtin_offsetof(struct percpu, syscall_scratch) == 24, "syscall.asm offset");
_Static_assert(__builtin_offsetof(struct percpu, self) == PERCPU_SELF_OFF, "this_cpu offset");

extern struct percpu g_cpus[SMP_MAX_CPUS];

static inline struct percpu *this_cpu(void) {
    return (struct percpu *)(uintptr_t)read_gs_qword(PERCPU_SELF_OFF);
}

/* Points GS_BASE at g_cpus[index] (after gdt_load, which zeroes it). */
static inline void percpu_bind(uint32_t index) {
    struct percpu *c = &g_cpus[index];
    c->self = c;
    c->index = index;
    wrmsr(MSR_GS_BASE, (uint64_t)(uintptr_t)c);
    wrmsr(MSR_KERNEL_GS_BASE, 0);   /* what ring 3 gets at its first swapgs */
    wrmsr(MSR_TSC_AUX, index);
}
#endif
