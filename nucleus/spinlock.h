/* spinlock.h - IRQ-saving spinlocks. On one CPU the interrupt-off part is
 * what gives mutual exclusion; the atomic flag makes the same code correct
 * for SMP later and turns "re-entered while held" into a loud panic instead
 * of a silent hang. Never hold one across anything that can block. */
#ifndef NUCLEUS_SPINLOCK_H
#define NUCLEUS_SPINLOCK_H
#include <stdint.h>
#include "cpu.h"

struct spinlock { volatile uint32_t locked; const char *name; };

#define SPINLOCK_INIT(n) { 0, n }

void spin_deadlock(const struct spinlock *l) __attribute__((noreturn));   /* spinlock.c */

static inline uint64_t spin_lock_irqsave(struct spinlock *l) {
    uint64_t f = irq_save();
    for (uint32_t spins = 0; __atomic_exchange_n(&l->locked, 1, __ATOMIC_ACQUIRE); spins++) {
        if (spins > 100000000u) spin_deadlock(l);
        __asm__ volatile("pause");
    }
    return f;
}

/* Acquire with interrupts already off (the scheduler's lock, nested locks). */
static inline void spin_lock(struct spinlock *l) {
    for (uint32_t spins = 0; __atomic_exchange_n(&l->locked, 1, __ATOMIC_ACQUIRE); spins++) {
        if (spins > 100000000u) spin_deadlock(l);
        __asm__ volatile("pause");
    }
}

/* kprintf's guard (W20-S06): one line at a time across CPUs. The spin is
 * bounded, so a panic while a line is half out still prints. */
uint64_t kprint_enter(void);
void kprint_leave(uint64_t flags);

/* Release without restoring IF (for handing the CPU over while still atomic). */
static inline void spin_unlock(struct spinlock *l) {
    __atomic_store_n(&l->locked, 0, __ATOMIC_RELEASE);
}

static inline void spin_unlock_irqrestore(struct spinlock *l, uint64_t flags) {
    spin_unlock(l);
    irq_restore(flags);
}
#endif
