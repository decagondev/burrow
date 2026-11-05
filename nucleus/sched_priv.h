/* sched_priv.h - what sched.c (switching, blocking, the tick) and thread.c
 * (creating, joining, finding threads) share. Never included outside those
 * two files. W20-S06, ADR-0011: one run queue under g_sched_lock. */
#ifndef NUCLEUS_SCHED_PRIV_H
#define NUCLEUS_SCHED_PRIV_H
#include "sched.h"
#include "spinlock.h"

/* Kernel stacks: slot i at KSTACK_REGION + i * KSTACK_SLOT; the first page of
 * each slot stays unmapped (guard). Upper half: shared by every address space. */
#define KSTACK_REGION 0xFFFFFFFFC0000000ull
#define KSTACK_SLOT   (KSTACK_BYTES + PAGE_4K)
#define PIN_ANY       (-1)

extern struct thread g_threads[THREADS_MAX];
/* Every thread's state, g_next_id, and each CPU's current/idle choice. Taken
 * with IF=0; held across switch_to and released by the thread switched to. */
extern struct spinlock g_sched_lock;
extern uint32_t g_next_id;

void sched_kick(const struct thread *t);   /* g_sched_lock held: wake an idle CPU for t */
void sched_on_tick(void);                  /* time.c's hook on every CPU */
void sched_start_tail(void);               /* a new thread drops the lock it was switched to with */
#endif
