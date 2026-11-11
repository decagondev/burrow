/* sched.h - kernel threads and the scheduler: preemptive round robin with a
 * 10 ms timeslice from each CPU's 1 kHz tick, one run queue (g_threads under
 * one spinlock) for every CPU (W20-S06, ADR-0011). Every thread may run on
 * any CPU (since TLB shootdown, S07) except main and the idle threads.
 *
 * Every thread owns a 16 KiB kernel stack with an unmapped guard page below
 * it. Interrupts taken while it runs land on that stack (trap.asm), so a
 * thread can be switched out from inside an IRQ handler and resumed later.
 * PIT-22 checklist, done on every switch before switch.asm runs:
 * percpu kstack bounds, TSS.RSP0, FS_BASE, CR3 (a user thread's own page
 * tables; every other thread runs on the kernel's), LSTAR, the FPU area.
 * A thread migrates between switches; all of that is reloaded each time. */
#ifndef NUCLEUS_SCHED_H
#define NUCLEUS_SCHED_H
#include "fpu.h"
#include <stdint.h>

#define THREADS_MAX      64u
#define KSTACK_BYTES     16384u
#define TIMESLICE_TICKS  10u

enum thread_state { T_FREE = 0, T_RUNNABLE, T_RUNNING, T_BLOCKED, T_SLEEPING, T_DEAD,
                    T_REAPING };   /* one joiner frees its stack (BUG-0023) */

struct thread {
    uint64_t rsp;               /* +0: saved by switch.asm; keep first */
    uint64_t kstack_bottom, kstack_top;
    uint64_t fs_base;
    uint64_t wake_ns;           /* T_SLEEPING, or a timed T_BLOCKED (0 = no timeout) */
    int      timed_out;         /* set when wake_ns expired a T_BLOCKED wait */
    enum thread_state state;
    uint32_t id;
    const char *name;
    struct thread *wait_next;   /* futex wait queue link (futex.c) */
    uint64_t futex_key;         /* address this thread waits on (futex.c) */
    uint64_t cr3;               /* page tables it runs on: the kernel's, or its process's (W9) */
    /* user threads (uthread.c): 0 for kernel and Go threads */
    uint64_t uspace;            /* address-space handle */
    int      killed;            /* nu_uthread_kill: leave at the next kernel exit */
    int      req_state;         /* its syscall or fault request (uthread.c) */
    uint64_t req[8];            /* tid, nr, args[6] as nu_syscall_wait copies them */
    long     req_result;
    struct thread *req_next;    /* request queue link */
    struct trap_frame *uframe;  /* its user registers while it waits for Go (W10) */
    int      interrupt;         /* nu_uthread_interrupt: stop at the next kernel exit */
    uint64_t lat[4];            /* round-trip stamps while traced (lattrace.h, W25-S01); lat[0] = 0: not traced */
    int      pin_cpu;           /* CPU index it may run on, -1 = any (sched_priv.h) */
    int      is_idle;           /* a CPU's idle thread: never picked by the queue */
    uint32_t cpu;               /* CPU index it last ran on */
    /* CPU time (cputime.c, W23-S02): written by the CPU running it */
    uint64_t user_ns, sys_ns;   /* in ring 3; in the kernel (an idle thread: idle) */
    uint64_t acct_mark;         /* when its current stint began, 0 = not yet */
    int      in_user;           /* a user thread in ring 3 */
    uint8_t  fx[FPU_AREA_BYTES] __attribute__((aligned(64)));   /* XSAVE (or FXSAVE) area (fpu.h) */
};
_Static_assert(__builtin_offsetof(struct thread, rsp) == 0, "switch.asm offset");

void sched_init(void);          /* the boot flow becomes thread 0 "main"; enables IRQs */
struct thread *thread_current(void);
uint32_t thread_id(const struct thread *t);
struct thread *thread_create(const char *name, void (*fn)(void *), void *arg);   /* 0 = no slot/memory */
struct trap_frame;
/* A thread whose first run restores *frame with IRETQ: every register, RIP,
 * RSP and RFLAGS as given (Go threads: go_load.c, clone in shim.c). */
struct thread *thread_create_from_frame(const char *name, const struct trap_frame *frame,
                                        uint64_t fs_base);
/* Like thread_create_from_frame, for ring 3: the thread runs on cr3 and
 * belongs to address space `uspace` (uthread.c). */
struct thread *thread_create_user(const char *name, const struct trap_frame *frame,
                                  uint64_t fs_base, uint64_t cr3, uint64_t uspace,
                                  const uint8_t *fx);   /* 512-byte FXSAVE image, 0 = fresh */
/* The thread with this id, or 0 (interrupts off while using the result). */
struct thread *thread_find(uint32_t id);
void thread_yield(void);
void thread_sleep_ns(uint64_t ns);
void thread_exit(void) __attribute__((noreturn));
void thread_join(struct thread *t);            /* waits for T_DEAD, then frees its stack */
/* The same for thread id, which slot t held when the caller found it; a
 * slot already freed or reused means another joiner was first (BUG-0023). */
void thread_join_id(struct thread *t, uint32_t id);
void thread_set_fs_base(struct thread *t, uint64_t fs);

/* For blocking primitives (futex.c). Call with interrupts OFF. */
void thread_block(uint64_t timeout_ns);        /* 0 = no timeout; sets current->timed_out */
struct spinlock;
/* Marks current blocked, THEN releases lock (IF stays off), then switches:
 * a waker that takes the lock afterwards always sees us blocked (PIT-23). */
void thread_block_unlock(uint64_t timeout_ns, struct spinlock *lock);
void thread_wake(struct thread *t);            /* T_BLOCKED -> T_RUNNABLE */
/* If t runs on another CPU, an IPI makes it enter the kernel now (its
 * return to ring 3 then sees killed / interrupt; W20-S07). */
void thread_poke(struct thread *t);
uint64_t sched_switch_count(void);
/* An AP's boot context becomes its idle thread (smp.c), then idles for ever. */
void sched_init_cpu(uint64_t kstack_bottom, uint64_t kstack_top);
void sched_idle(void) __attribute__((noreturn));
#endif
